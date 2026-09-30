package utility

import (
	"encoding/json"
	"net/http"
)

func ResponseJson(w http.ResponseWriter, v any, s int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s)
	json.NewEncoder(w).Encode(v)
}
