package handlers

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/fintech-bank-platform/pkg/tracing"
)

type ReadProxy struct {
	proxy *httputil.ReverseProxy
}

func NewReadProxy(upstream *url.URL, serviceName string) *ReadProxy {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = tracing.Transport(nil)
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
		response.FromError(w, apperrors.New("UPSTREAM_UNAVAILABLE", fmt.Sprintf("%s is unavailable", serviceName), http.StatusBadGateway).Wrap(err))
	}
	return &ReadProxy{proxy: proxy}
}

func (p *ReadProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.proxy.ServeHTTP(w, r)
}
