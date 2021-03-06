package web

import (
	"fmt"
	"github.com/0neSe7en/echo-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/pm-connect/log-shipper/monitoring"
	"github.com/pm-connect/log-shipper/web/api"
	"github.com/pm-connect/log-shipper/web/ui"
	log "github.com/sirupsen/logrus"
)

func StartServer(port int, monitor *monitoring.Monitor, includeUi bool, consulAddr string, serviceName string) {
	e := echo.New()

	e.HideBanner = true

	// Routes...
	e.Pre(middleware.RemoveTrailingSlash(), middleware.CORS())

	api.AddRoutes(e, monitor, consulAddr, serviceName)

	if includeUi {
		e.Use(
			echoprometheus.NewMetric(),
		)

		ui.AddRoutes(e)
		e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	}

	log.Fatal(e.Start(fmt.Sprintf(":%d", port)))
}
