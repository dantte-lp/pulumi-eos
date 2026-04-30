package provider

import (
	provider "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/resources/device"
)

// Namespace is the Pulumi package namespace registered with the engine.
// It maps onto resource tokens of the form `eos:<area>:<Resource>`.
const Namespace = "eos"

// New returns a configured pulumi-go-provider ready to be Run against the
// Pulumi engine.
func New() (provider.Provider, error) {
	return infer.NewProviderBuilder().
		WithNamespace(Namespace).
		WithDisplayName("Arista EOS").
		WithLicense("Apache-2.0").
		WithRepository("https://github.com/dantte-lp/pulumi-eos").
		WithHomepage("https://github.com/dantte-lp/pulumi-eos").
		WithDescription("Native Go Pulumi resource provider for Arista EOS and CloudVision (CVP / CVaaS).").
		WithKeywords("arista", "eos", "cloudvision", "cvp", "cvaas", "network", "gnmi", "eapi").
		WithLanguageMap(map[string]any{
			"go": map[string]string{
				"importBasePath": "github.com/dantte-lp/pulumi-eos/sdk/go/eos",
			},
			"nodejs": map[string]any{
				"packageName": "@dantte-lp/pulumi-eos",
			},
			"python": map[string]any{
				"packageName": "pulumi_eos",
			},
		}).
		WithConfig(infer.Config(&Config{})).
		WithResources(
			infer.Resource(&device.Device{}),
		).
		Build()
}
