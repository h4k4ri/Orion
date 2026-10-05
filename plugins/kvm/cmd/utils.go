package main

import "encoding/json"

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func decode(b []byte, v interface{}) error {
	return json.Unmarshal(b, v)
}
