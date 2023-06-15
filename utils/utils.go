package utils

import (
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/prometheus/client_golang/prometheus"
	"path/filepath"
	"strings"
)

func IdWithRegion(region, id string) string {
	if id == "" {
		return ""
	}
	if arn.IsARN(id) {
		a, _ := arn.Parse(id)
		region = a.Region
		id = a.Resource
		parts := strings.Split(a.Resource, ":")
		if len(parts) > 1 {
			id = parts[1]
		}
	}
	return region + "/" + id
}

func Filtered(filters, tags map[string]string) bool {
	for tagName, desiredValue := range filters {
		value := tags[tagName]
		if matched, _ := filepath.Match(desiredValue, value); !matched {
			return true
		}
	}
	return false
}

func Desc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc(name, help, labels, nil)
}

func Gauge(desc *prometheus.Desc, value float64, labels ...string) prometheus.Metric {
	return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
}

func Counter(desc *prometheus.Desc, value float64, labels ...string) prometheus.Metric {
	return prometheus.MustNewConstMetric(desc, prometheus.CounterValue, value, labels...)
}
