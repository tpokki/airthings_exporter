package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	kingpin "github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/go-resty/resty/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const (
	namespace = "airthings"
)

type metricInfo struct {
	Desc *prometheus.Desc
	Type prometheus.ValueType
}

type metrics map[AirthingsMetric]metricInfo

type Exporter struct {
	tokenSource oauth2.TokenSource
	resty       *resty.Client
	mutex       sync.RWMutex
	metrics     metrics
	logger      log.Logger

	deviceConfig *DeviceConfig
}

type DeviceConfig struct {
	lastUpdate time.Time
	devices    []*Device
}

type Device struct {
	id       string
	segment  string
	location string
}

var (
	labelNames      = []string{"device", "segment", "location"}
	airthingMetrics = metrics{
		Battery:     newMetric("battery", "Battery charge capacity", prometheus.GaugeValue, nil),
		CO2:         newMetric("co2", "CO2 levels", prometheus.GaugeValue, nil),
		Humidity:    newMetric("humidity", "Humidity", prometheus.GaugeValue, nil),
		Pm1:         newMetric("pm1", "Extremely fine particles, less than 1 microns", prometheus.GaugeValue, nil),
		Pm25:        newMetric("pm25", "Fine particles, less than 2.5 microns", prometheus.GaugeValue, nil),
		Pressure:    newMetric("air_pressure", "Pressure", prometheus.GaugeValue, nil),
		Radon:       newMetric("radon", "Radon, short term average", prometheus.GaugeValue, nil),
		Temperature: newMetric("temperature", "Temperature", prometheus.GaugeValue, nil),
		VOC:         newMetric("voc", "Volatile organic compounds", prometheus.GaugeValue, nil),
	}
)

func (e *Exporter) discover() {
	if e.deviceConfig.updateRequired() {
		defer func() { e.deviceConfig.lastUpdate = time.Now() }()

		token, err := e.tokenSource.Token()
		if err != nil {
			level.Error(e.logger).Log("msg", "failed to get access token, will try again later", "err", err)
			return
		}

		resp, err := e.resty.R().
			SetAuthToken(token.AccessToken).
			SetResult(&AirthingsDevicesResult{}).
			Get("https://ext-api.airthings.com/v1/devices")

		if err != nil {
			level.Error(e.logger).Log("msg", "failed to get devices, will try again later", "err", err)
			return
		}

		result := resp.Result().(*AirthingsDevicesResult)
		var devices []*Device
		for _, r := range result.Devices {
			level.Info(e.logger).Log("msg", "updating device", "device", r.Id, "segment", r.Segment.Name, "location", r.Location.Name)
			devices = append(devices, &Device{r.Id, r.Segment.Name, r.Location.Name})
		}
		e.deviceConfig.devices = devices
	}
}

func (dc *DeviceConfig) updateRequired() bool {
	return dc.lastUpdate.IsZero() ||
		time.Since(dc.lastUpdate).Minutes() > 30
}

func (e *Exporter) retrieveMetrics(ch chan<- prometheus.Metric) {
	token, err := e.tokenSource.Token()
	if err != nil {
		level.Error(e.logger).Log("msg", "failed to get access token, will try again later", "err", err)
		return
	}

	for _, d := range e.deviceConfig.devices {
		resp, err := e.resty.R().
			SetAuthToken(token.AccessToken).
			SetPathParams(map[string]string{
				"serialNumber": d.id,
			}).
			SetResult(&AirthingsMetricsResult{}).
			Get("https://ext-api.airthings.com/v1/devices/{serialNumber}/latest-samples")

		if err != nil {
			level.Error(e.logger).Log("msg", "failed to get metrics for device", "device", d.id, "err", err)
			continue
		}

		result := resp.Result().(*AirthingsMetricsResult)
		level.Debug(e.logger).Log("msg", "airthings metrices received", "device", d.id)

		for metric, data := range result.Data {
			if atm, ok := e.metrics[metric]; ok {
				if value, ok := data.(float64); ok {
					ch <- prometheus.MustNewConstMetric(atm.Desc, atm.Type, value, d.id, d.segment, d.location)
				}
			}
		}
	}
}

// Collect fetches the stats from airthings API and delivers them
// as Prometheus metrics. It implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.discover()
	e.retrieveMetrics(ch)
}

// Describe describes all the metrics ever exported by the Airthings exporter. It
// implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range airthingMetrics {
		ch <- m.Desc
	}
}

func main() {
	var (
		webConfig = webflag.AddFlags(kingpin.CommandLine, ":9101")

		clientId     = kingpin.Flag("airthings.cloud.auth.client.id", "Airthings Cloud API Client ID").String()
		clientSecret = kingpin.Flag("airthings.cloud.auth.client.secret", "Airthings Cloud API Client Secret").String()
		authScopes   = kingpin.Flag("airthings.cloud.auth.scopes", "Airthings Cloud API Scopes").Default("read:device:current_values").String()
		tokenUrl     = kingpin.Flag("airthings.cloud.auth.url", "Airthings Cloud API Token URL").Default("https://accounts-api.airthings.com/v1/token").URL()
	)

	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(version.Print("airthings_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	ctx := context.Background()
	conf := &clientcredentials.Config{
		ClientID:     *clientId,
		ClientSecret: *clientSecret,
		Scopes:       strings.Split(*authScopes, ","),
		TokenURL:     (*tokenUrl).String(),
	}

	exporter := &Exporter{
		mutex:        sync.RWMutex{},
		tokenSource:  conf.TokenSource(ctx),
		resty:        resty.New(),
		metrics:      airthingMetrics,
		logger:       logger,
		deviceConfig: &DeviceConfig{},
	}

	prometheus.MustRegister(exporter)
	prometheus.MustRegister(version.NewCollector("airthings_exporter"))

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Airthings Exporter</title></head>
             <body>
             <h1>Airthings Exporter</h1>
             <p><a href="/metrics">Metrics</a></p>
             </body>
             </html>`))
	})
	srv := &http.Server{}
	if err := web.ListenAndServe(srv, webConfig, logger); err != nil {
		level.Error(logger).Log("msg", "Error starting HTTP server", "err", err)
		os.Exit(1)
	}
}

func newMetric(metricName string, docString string, t prometheus.ValueType, constLabels prometheus.Labels) metricInfo {
	return metricInfo{
		Desc: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cloud", metricName),
			docString,
			labelNames,
			constLabels,
		),
		Type: t,
	}
}
