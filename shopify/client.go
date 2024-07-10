package shopify

import (
	"encoding/json"
	"io"

	"net/http"

	"github.com/chewycrunch/shopify-monitor/utils"
)

func FetchProductData(shopifyBaseUrl string) ([]utils.Product, error) {
	resp, err := http.Get(shopifyBaseUrl + "/products.json")
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var productsResponse utils.ProductsResponse
	err = json.Unmarshal(body, &productsResponse)
	if err != nil {
		return nil, err
	}

	return productsResponse.Products, nil
}
