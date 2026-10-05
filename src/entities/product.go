package entities

type Product struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int32   `json:"stock"`
}

type AdjustStockRequest struct {
	Delta int32 `json:"delta" binding:"required"` // != 0; positif = kurangi stok, negatif = tambah
}
