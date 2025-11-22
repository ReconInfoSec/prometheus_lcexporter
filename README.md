# prometheus-lcexporter
Howdy, this is a Prometheus exporter for LimaCharlie.  It's designed to hit the LimaCharlie API for 1-n organizations, download configurable statistics, and then make those stats available for scraping into a Prometheus database.

## Configuration
There are two configuration files used for this exporter.  One to define the specifics of gathering the metrics and another for your organization(s).

### limacharlie.yaml
This yaml file is pretty well ready to go, but you will need to set credentials (`uid` and `secret`) at the very least.  After that feel free to look through the configuration.  Right now it pulls the online sensor count and the current quota as gauges and the log bytes, output bytes, sensor events, sensor times, and USP bytes as counters (for what constitues a gauge vs. a counter see the Prometheus documentation).  You can add additional metric endpoints that you identify as useful, but these are the only ones I've tested so far.  It is set to hit the APIs every 10 minutes to pull new statistics.  If you have a small number of organizations and want greater fidelity you can decrease this number.

### org.json
I'm aware it seems odd to have one file be yaml and the other json, I may make the limacharlie.yaml file json in the future for consistancy.  That said, this file is also very simple.  The sample shows the format, it is an array of dicts that have the `oid` and `name` fields avaiable to you.  The `oid` is the LimaCharlie OID, the `name` can be a "pretty" name, it is the name that the metrics will be labeled with in Prometheus.  The array can be as long as you need it to be, depending on how many orgs you are supporting.

## Building/using
Using the included Dockerfile you should be able to get up and running fairly quickly.  Get your configuration files set and a `docker build -t lc_exporter:latest .` shouild give you a runable image.  Remember that if you're building your docker image to run on a different architecture than the one you're building from you may need to set the `DOCKER_DEFAULT_PLATFORM` environment variable first.  In its simplest form you can run the container with a `docker run -p 31337:31337 -d lc_exporter:latest` but you may want/need to add additional options depending on your situation.

