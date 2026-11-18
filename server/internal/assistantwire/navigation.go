package assistantwire

// navigationRoutes are the user's navigation choices (the start page, #9071): the default manager keeps them in its
// settings (navigation.go there); the hub relays them like Routes, and the apps hear of a change as the `navigation`
// ws.broadcast.
var navigationRoutes = []string{
	"GET /api/v1/navigation",
	"PUT /api/v1/navigation",
}

func init() { Routes = append(Routes, navigationRoutes...) }
