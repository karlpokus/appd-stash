# sla-vault
Retain high resolution perf data from appdynamics
Configure:  create InfluxDB database and configure one or more data sources in config.json
Get:        *one minute* Business Transaction(BT) *scorecard* data from one or several data sources
Save:       one minute  *scorecard* into InfluxDB with possibility to calculate *SLA* -> calcSLA()
(Graph:)    can easily graph *SLA* from saved *scorecard* with your favorite tool Grafana/Graphite etc.