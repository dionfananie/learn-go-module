package entities

type Product struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int32   `json:"stock"`
}

type AdjustStockRequest struct {
	Delta int32 `json:"delta" binding:"required,ne=0"` // != 0; positif = kurangi stok, negatif = tambah
}

// TransferStockRequest memindahkan sejumlah stok dari satu produk ke produk lain.
// from_id != to_id, amount > 0.
type TransferStockRequest struct {
	FromID int   `json:"from_id" binding:"required,gt=0"`
	ToID   int   `json:"to_id" binding:"required,gt=0"`
	Amount int32 `json:"amount" binding:"required,gt=0"`
}
