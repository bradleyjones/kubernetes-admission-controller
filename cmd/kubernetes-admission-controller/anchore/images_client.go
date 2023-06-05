package anchore

import (
	"context"
	_nethttp "net/http"

	anchoreEngine "github.com/anchore/enterprise-client-go/pkg/engine"
	anchoreEnterprise "github.com/anchore/enterprise-client-go/pkg/enterprise"
)

// imagesClient abstracts the Anchore Go client's images service and exposes
// only the client operations needed by this application.
type imagesClient interface {
	AddImage(
		ctx context.Context,
		image anchoreEngine.ImageAnalysisRequest,
		localVarOptionals *anchoreEnterprise.AddImageOpts,
	) ([]anchoreEngine.AnchoreImage, *_nethttp.Response, error)
	ListImages(
		ctx context.Context,
		localVarOptionals *anchoreEnterprise.ListImagesOpts,
	) ([]anchoreEngine.AnchoreImage, *_nethttp.Response, error)
	GetImagePolicyCheck(
		ctx context.Context,
		imageDigest string,
		tag string,
		localVarOptionals *anchoreEnterprise.GetImagePolicyCheckOpts,
	) ([]map[string]interface{}, *_nethttp.Response, error)
}
