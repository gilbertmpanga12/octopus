package chttp

import (
	"fmt"
	"net/http"
)

// Handler is an http.Handler that renders a chttp.Response
type Handler func(*http.Request) Response

// HandlerFunc wraps a `chttp.Handler` in a standard `http` handler
func (h Handler) HandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res := h(r)
		bs, err := res.Marshal()

		if err != nil {
			fmt.Println("INTERNAL DECODING ERROR: ", err, string(res.Data()))
			panic(err)
		}

		w.WriteHeader(res.HTTPCode())
		_, err = w.Write(bs)

		if err != nil {
			panic(err)
		}
	}
}
