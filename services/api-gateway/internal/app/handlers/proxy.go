package handlers

import (
	"errors"
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
	return newReadProxy(upstream, serviceName, nil)
}

func NewGuardedReadProxy(upstream *url.URL, serviceName string, guard *AccessGuard, access ReadAccess) *ReadProxy {
	return newReadProxy(upstream, serviceName, func(resp *http.Response) error {
		return guard.releaseOwned(resp, access)
	})
}

func newReadProxy(upstream *url.URL, serviceName string, release func(*http.Response) error) *ReadProxy {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = tracing.Transport(nil)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.Host = upstream.Host
		req.Header.Del("Authorization")
		if release != nil {
			req.Header.Del("Accept-Encoding")
		}
		req.Header.Set(middleware.RequestIDHeader, middleware.GetRequestID(req.Context()))
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del(middleware.RequestIDHeader)
		if release == nil || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil
		}
		return release(resp)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			response.FromError(w, appErr)
			return
		}
		response.FromError(w, apperrors.New("UPSTREAM_UNAVAILABLE", fmt.Sprintf("%s is unavailable", serviceName), http.StatusBadGateway).Wrap(err))
	}
	return &ReadProxy{proxy: proxy}
}

func (p *ReadProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.proxy.ServeHTTP(w, r)
}
