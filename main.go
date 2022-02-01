package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/coroot/coroot-aws-agent/rds"
	"github.com/coroot/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/alecthomas/kingpin.v2"
	"net/http"
	_ "net/http/pprof"
)

var version = "unknown"

func main() {
	awsRegion := kingpin.Flag("aws-region", "AWS region (env: AWS_REGION)").Envar("AWS_REGION").Required().String()
	awsApiTimeout := kingpin.Flag("aws-api-timeout", "AWS API timeout").Default("30s").Duration()
	rdsDiscoveryInterval := kingpin.Flag("rds-discovery-interval", "RDS discovery interval").Default("60s").Duration()
	rdsDbUser := kingpin.Flag("rds-db-user", "RDS db user (env: RDS_DB_USER)").Envar("RDS_DB_USER").String()
	rdsDbPassword := kingpin.Flag("rds-db-password", "RDS db password (env: RDS_DB_PASSWORD)").Envar("RDS_DB_PASSWORD").String()
	rdsDbConnectTimeout := kingpin.Flag("rds-db-connect-timeout", "RDS db connect timeout").Default("1s").Duration()
	rdsDbQueryTimeout := kingpin.Flag("rds-db-query-timeout", "RDS db query timeout").Default("30s").Duration()
	rdsLogsScrapeInterval := kingpin.Flag("rds-logs-scrape-interval", "RDS logs scrape interval (0 to disable)").Default("30s").Duration()
	listenAddress := kingpin.Flag("listen-address", `Listen address (env: LISTEN_ADDRESS) - "<ip>:<port>" or ":<port>".`).Envar("LISTEN_ADDRESS").Default("0.0.0.0:80").String()
	kingpin.HelpFlag.Short('h').Hidden()
	kingpin.Version(version)
	kingpin.Parse()

	log := logger.NewKlog("")

	cfg := aws.NewConfig().WithRegion(*awsRegion)
	cfg = cfg.WithHTTPClient(&http.Client{Timeout: *awsApiTimeout})
	awsSession, err := session.NewSession(cfg)
	if err != nil {
		log.Error(err)
		return
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(info("aws_agent_info", version))

	rdsDbConf := rds.DbCollectorConf{
		User:           *rdsDbUser,
		Password:       *rdsDbPassword,
		ConnectTimeout: *rdsDbConnectTimeout,
		QueryTimeout:   *rdsDbQueryTimeout,

		LogsScrapeInterval: *rdsLogsScrapeInterval,
	}
	go rds.NewDiscoverer(reg, awsSession, *rdsDiscoveryInterval, rdsDbConf).Run()

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	log.Info("listening on:", *listenAddress)
	log.Error(http.ListenAndServe(*listenAddress, nil))
}

func info(name, version string) prometheus.Collector {
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        name,
		ConstLabels: prometheus.Labels{"version": version},
	})
	g.Set(1)
	return g
}
