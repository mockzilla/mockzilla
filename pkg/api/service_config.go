package api

// CreateServiceConfigRoutes used to serve each service's effective config at
// /.config. That config carries upstream headers, which hold credentials, so the
// route is gone.
//
// Deprecated: it registers nothing. It remains so a server that calls it still builds.
func CreateServiceConfigRoutes(_ *Router) error {
	return nil
}
