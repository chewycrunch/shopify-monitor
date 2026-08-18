package utils

type Variant struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Available bool   `json:"available"`
}

type Product struct {
	ID       int64     `json:"id"`
	Title    string    `json:"title"`
	Handle   string    `json:"handle"`
	Variants []Variant `json:"variants"`
	Images   []Image   `json:"images"`
}

type Image struct {
	ID  int64  `json:"id"`
	Src string `json:"src"`
}

type ProductsResponse struct {
	Products []Product `json:"products"`
}
