package monitor

type Image struct {
	Src string `json:"src"`
}

type Variant struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Available bool   `json:"available"`
	Price     string `json:"price"`
}

type Product struct {
	Title    string    `json:"title"`
	Variants []Variant `json:"variants"`
	Images   []Image   `json:"images"`
}
