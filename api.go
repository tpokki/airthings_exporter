package main

type AirthingsMetric string

const (
	Battery     AirthingsMetric = "battery"
	CO2         AirthingsMetric = "co2"
	Humidity    AirthingsMetric = "humidity"
	Pm1         AirthingsMetric = "pm1"
	Pm25        AirthingsMetric = "pm25"
	Pressure    AirthingsMetric = "pressure"
	Radon       AirthingsMetric = "radonShortTermAvg"
	Temperature AirthingsMetric = "temp"
	VOC         AirthingsMetric = "voc"
	DeviceType  AirthingsMetric = "relayDeviceType"
)

type AirthingsMetricsResult struct {
	Data map[AirthingsMetric]any `json:"data"`
}

type AirthingsDevicesResult struct {
	Devices []AirthingDevice `json:"devices"`
}

type AirthingDevice struct {
	Id         string            `json:"id"`
	DeviceType string            `json:"deviceType"`
	Sensors    []string          `json:"sensors"`
	Segment    AirthingsSegment  `json:"segment"`
	Location   AirthingsLocation `json:"location"`
}

type AirthingsSegment struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type AirthingsLocation struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}
