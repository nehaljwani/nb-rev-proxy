# NextBus Reverse Proxy Service

This is a small application for proxying and caching responses to API calls made to https://webservices.nextbus.com/service/publicXMLFeed

## Implementation Details

* Written in Go instead of Python since handling multi processing is much easier in Go.
* Uses the labstack echo web framework, since it provides an easy way to handle api versioning using groups.
* DB and cache updates are goroutines, so that they don't block the user.
* Two types of caching mechanims have been incorporated: Local (go-cache) and Remote (redis). Both are in-memory and the former is _faster_ since it is native and doesn't involve any network overhead. Remote is preferred when the app needs to be horizontally scaled behind a load balancer.
* Request logging and stats collection is being done using middlewares.
* The app requires the db to be up before spawning itself, which is done using the famous _wait-for-it.sh_ script provided by [@vishnubob](https://github.com/vishnubob/)
* Instead of turning off SELinux, the context `svirt_sandbox_file_t` has to be set on the database `datadir`
  
## Assumptions 

* List of endpoints includes the route `/api/v1/stats` too.
* 'Response Time' refers to the time taken to serve any end point (by the proxy app), which also includes the time taken to fetch something from cache, if present.
* Containers are allowed to have access to the docker daemon socket.
