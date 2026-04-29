package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/loader"

	// Import services to trigger their init() registration
	_ "example/services/petstore"
	_ "example/services/spoonacular"
)

func main() {
	// Create the router
	router := api.NewRouter()

	// Load all registered services
	loader.LoadAll(router)

	// Log discovered services
	services := loader.DefaultRegistry.List()
	if len(services) == 0 {
		log.Println("WARNING: No services discovered!")
	} else {
		log.Printf("Discovered %d service(s): %v", len(services), services)
	}

	// Configure server
	port := os.Getenv("PORT")
	if port == "" {
		port = "2200"
	}
	addr := fmt.Sprintf(":%s", port)

	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Starting server on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server failed: %v", err)
	}
}
