# Coroot-aws-agent

Coroot-aws-agent is an open-source prometheus exporter that gathers metrics from AWS services.

|Serivce|Description|
|-|-|
|RDS for Postgres (including Aurora)|autodiscovery, OS metrics from Enhanced Monitoring, Postgres metrics, metrics from logs|
|RDS for Mysql (including Aurora)|coming soon|
|EBS|coming soon|

## Credentials and permissions

### Create IAM policy

Coroot-aws-agent requires permissions to describe RDS instances, read their logs and read Enhanced Monitoring data from CloudWatch.

**MonitoringReadOnlyAccess**

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "rds:DescribeDBInstances",
                "rds:DescribeDBLogFiles",
                "rds:DownloadDBLogFilePortion"
            ],
            "Resource": [
                "*"
            ]
        },
        {
            "Effect": "Allow",
            "Action": [
                "logs:GetLogEvents"
            ],
            "Resource": [
                "arn:aws:logs:*:*:log-group:RDSOSMetrics:log-stream:*"
            ]
        }
    ]
}
```

### Attach IAM policy

Coroot-aws-agent uses [default credential provider chain](https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html#specifying-credentials) to find AWS credentials.

Here are the most popular options:
* create an IAM user with programmatic access, attach the policy to it and use `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` environment variables
* attach an IAM role with the policy to an EC2 instance where the agent will be run on
* attach the policy to a Kubernetes [service account](https://eksctl.io/usage/iamserviceaccounts/) and assign it to the agent

## RDS for Postgresql

The agent discovers RDS instances in the specified region and gathers OS, Postgres and log metrics for each of them.

The collected metrics are described [here](https://coroot.com/docs/metrics/aws-agent).

### Create a database role

    create role <USER> with login password '<PASSWORD>';
    grant pg_monitor to <USER>;

IAM database authentication is coming soon.

### Enable pg_stat_statements

    create extension pg_stat_statements;
    select * from pg_stat_statements; -- to check

The `pg_stat_statements` extension should be loaded via the `shared_preload_libraries` server setting.

## Run

### Kubernetes

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: coroot

---

apiVersion: apps/v1
kind: Deployment
metadata:
  name: coroot-aws-agent
  namespace: coroot
spec:
  selector:
    matchLabels: {app: coroot-aws-agent}
  replicas: 1
  template:
    metadata:
      labels: {app: coroot-aws-agent}
      annotations:
        prometheus.io/scrape: 'true'
        prometheus.io/port: '80'
    spec:
      containers:
        - name: coroot-aws-agent
          image: ghcr.io/coroot/coroot-aws-agent:latest
          ports:
            - containerPort: 80
              name: http
          env:
            - name: AWS_REGION
              value: <REGION>
            - name: AWS_ACCESS_KEY_ID
              value: <KEY>
            - name: AWS_SECRET_ACCESS_KEY
              value: <SECRET>
            - name: RDS_DB_USER
              value: <USER>
            - name: RDS_DB_PASSWORD
              value: <PASSWORD>
```

If you use [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator), 
you will also need to create a PodMonitor:
```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: coroot-aws-agent
  namespace: coroot
spec:
  selector:
    matchLabels:
      app: coroot-aws-agent
  podMetricsEndpoints:
    - port: http
```

Make sure the PodMonitor matches `podMonitorSelector` defined in your Prometheus:
```yaml
apiVersion: monitoring.coreos.com/v1
kind: Prometheus
...
spec:
  ...
  podMonitorNamespaceSelector: {}
  podMonitorSelector: {}
  ...
```
The special value `{}` allows Prometheus to watch all the PodMonitors from all namespaces. 

### Docker

    docker run --detach --name coroot-aws-agent \
        -e AWS_REGION=<REGION> \
        -e AWS_ACCESS_KEY_ID=<KEY> \
        -e AWS_SECRET_ACCESS_KEY=<SECRET> \
        -e RDS_DB_USER=<USER> \
        -e RDS_DB_PASSWORD=<PASSWORD> \
        ghcr.io/coroot/coroot-aws-agent

## License

Coroot-aws-agent is licensed under the [Apache License, Version 2.0](https://github.com/coroot/coroot-aws-agent/blob/main/LICENSE).
