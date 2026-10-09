package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"terraform-azure-simulator/internal/provider"
)

func main() {
	err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
		Address: "terraform.local/learning/azuresim",
	})
	if err != nil {
		log.Fatal(err)
	}
}
