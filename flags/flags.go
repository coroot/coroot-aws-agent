package flags

import "gopkg.in/alecthomas/kingpin.v2"

var (
	AwsRegion                 = kingpin.Flag("aws-region", "AWS region (env: AWS_REGION)").Envar("AWS_REGION").Required().String()
	DiscoveryInterval         = kingpin.Flag("discovery-interval", "discovery interval").Default("60s").Duration()
	RdsDbUser                 = kingpin.Flag("rds-db-user", "RDS db user (env: RDS_DB_USER)").Envar("RDS_DB_USER").String()
	RdsDbPassword             = kingpin.Flag("rds-db-password", "RDS db password (env: RDS_DB_PASSWORD)").Envar("RDS_DB_PASSWORD").String()
	RdsDbConnectTimeout       = kingpin.Flag("rds-db-connect-timeout", "RDS db connect timeout").Default("1s").Duration()
	RdsDbQueryTimeout         = kingpin.Flag("rds-db-query-timeout", "RDS db query timeout").Default("30s").Duration()
	RdsLogsScrapeInterval     = kingpin.Flag("rds-logs-scrape-interval", "RDS logs scrape interval (0 to disable)").Default("30s").Duration()
	DbScrapeInterval          = kingpin.Flag("db-scrape-interval", "How often to scrape DB system views").Default("30s").Duration()
	ElasticacheConnectTimeout = kingpin.Flag("ec-connect-timeout", "Elasticache connect timeout").Default("1s").Duration()
	ElasticacheFilters        = kingpin.Flag("ec-filter", `a tag_name:tag_value pair for filtering EC instances by their tags while discovery (env: EC_FILTER)`).Envar("EC_FILTER").StringMap()
	RdsFilters                = kingpin.Flag("rds-filter", `a tag_name:tag_value pair for filtering RDS instances by their tags while discovery (env: RDS_FILTER)`).Envar("RDS_FILTER").StringMap()
	ListenAddress             = kingpin.Flag("listen-address", `Listen address (env: LISTEN_ADDRESS) - "<ip>:<port>" or ":<port>".`).Envar("LISTEN_ADDRESS").Default("0.0.0.0:80").String()
)
