package payload

import "encoding/json"

var _ = json.Marshal

type UserDTO struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}