package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/coroot/coroot-aws-agent/elasticache"
	"github.com/coroot/coroot-aws-agent/flags"
	"github.com/coroot/coroot-aws-agent/rds"
	"github.com/coroot/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/alecthomas/kingpin.v2"
	"net/http"
	_ "net/http/pprof"
	"time"
)

var version = "unknown"

func main() {
	kingpin.HelpFlag.Short('h').Hidden()
	kingpin.Version(version)
	kingpin.Parse()

	log := logger.NewKlog("")

	cfg := aws.NewConfig().WithRegion(*flags.AwsRegion)
	cfg.Retryer = client.DefaultRetryer{
		NumMaxRetries:    5,
		MinRetryDelay:    500 * time.Millisecond,
		MaxRetryDelay:    10 * time.Second,
		MinThrottleDelay: 500 * time.Millisecond,
		MaxThrottleDelay: 10 * time.Second,
	}
	awsSession, err := session.NewSession(cfg)
	if err != nil {
		log.Error(err)
		return
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(info("aws_agent_info", version))

	go rds.NewDiscoverer(reg, awsSession).Run()
	go elasticache.NewDiscoverer(reg, awsSession).Run()

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	log.Info("listening on:", *flags.ListenAddress)
	log.Error(http.ListenAndServe(*flags.ListenAddress, nil))
}

func info(name, version string) prometheus.Collector {
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		ConstLabels: prometheus.Labels{"version": version},
	})
	g.Set(1)
	return g
}
