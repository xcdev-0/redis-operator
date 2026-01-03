package services

// exporterPortProvider return the exporter port if bool is true
type ExporterPortProvider func() (port int, enable bool)

var DisableMetrics ExporterPortProvider = func() (int, bool) {
	return 0, false
}
