package handlers

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/response"
)

type ReadProxy struct {
	proxy *httputil.ReverseProxy
}

func NewReadProxy(upstream *url.URL) *ReadProxy {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.Host = upstream.Host
		req.Header.Set(middleware.RequestIDHeader, middleware.GetRequestID(req.Context()))
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del(middleware.RequestIDHeader)
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		response.FromError(w, apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway).Wrap(err))
	}
	return &ReadProxy{proxy: proxy}
}

func (p *ReadProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.proxy.ServeHTTP(w, r)
}
