package main

import (
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

// A Client sends http.Requests and returns http.Responses or errors in case of failure.
type Client interface {
	Do(*http.Request) (*http.Response, error)
}

// ClientFunc is a function type that implements the Client interface.
type ClientFunc func(*http.Request) (*http.Response, error)

// Do returns the ClientFunc
func (f ClientFunc) Do(r *http.Request) (*http.Response, error) {
	return f(r)
}

// A Decorator is function type that wraps a Client with extra functionality.
type Decorator func(Client) Client

// Logging returns a Decorator that logs a Client's requests.
// Orthogonal concern 1: logging
func Logging(l *log.Logger) Decorator {
	return func(c Client) Client {
		return ClientFunc(func(r *http.Request) (*http.Response, error) {
			l.Printf("%s: %s %s", r.UserAgent, r.Method, r.URL)
			return c.Do(r)
		})
	}
}

// Instrumentation returns a Decorator that instruments a Client with the given metrics.
// Orthogonal concern 2: instrumentation
func Instrumentation(requests Counter, latency Histogram) Decorator {
	return func(c Client) Client {
		return ClientFunc(func(r *http.Request) (*http.Response, error) {
			defer func(start time.Time) {
				latency.Observe(time.Since(start).Nanoseconds())
				requests.Add(1)
			}(time.Now())
			return c.Do(r)
		})
	}
}

// FaultTolerance returns a Decorator that extends a Client with fault tolerance
// configured with the given attempts and backoff duration.
// Orthogonal concern 3: fault tolerance
func FaultTolerance(attempts int, backoff time.Duration) Decorator {
	return func(c Client) Client {
		return ClientFunc(func(r *http.Request) (res *http.Response, err error) {
			for i := 0; i <= attempts; i++ {
				if res, err = c.Do(r); err == nil {
					break
				}
				time.Sleep(backoff * time.Duration(i))
			}
			return res, err
		})
	}
}

// Authorization returns a Decorator that authorizes every Client request
// with the given token.
// Orthogonal concern 4: authorization
func Authorization(token string) Decorator {
	return Header("Authorization", token)
}

// Header returns a Decorator that adds the given HTTP header to every request
// done by a Client.
func Header(name, value string) Decorator {
	return func(c Client) Client {
		return ClientFunc(func(r *http.Request) (*http.Response, error) {
			r.Header.Add(name, value)
			return c.Do(r)
		})
	}
}

// LoadBalancing returns a Decorator that load balances a Client's requests across
// multiple backends using the given Director.
// Orthogonal concern 5: load balancing
func LoadBalancing(dir Director) Decorator {
	return func(c Client) Client {
		return ClientFunc(func(r *http.Request) (*http.Response, error) {
			dir(r)
			return c.Do(r)
		})
	}
}

// A Director modifies an http.Request to follow a load balancing strategy.
type Director func(*http.Request)

// RoundRobin returns a Balancer which round-robins across the given backends.
func RoundRobin(robin uint64, backends ...string) Director {
	return func(r *http.Request) {
		if len(backends) > 0 {
			r.URL.Host = backends[atomic.AddUint64(&robin, 1)%uint64(len(backends))]
		}
	}
}

// Random returns a Balancer which randomly picks one of the given backends.
func Random(seed int64, backends ...string) Director {
	rnd := rand.New(rand.NewSource(seed))
	return func(r *http.Request) {
		if len(backends) > 0 {
			r.URL.Host = backends[rnd.Intn(len(backends))]
		}
	}
}

// Decorate decorates a Client c with all the given Decorators, in order.
func Decorate(c Client, ds ...Decorator) Client {
	decorated := c
	for _, decorate := range ds {
		decorated = decorate(decorated)
	}
	return decorated
}

func decoratorMain() {
	cli := Decorate(http.DefaultClient,
		Authorization("authorizationtokengoeshere"),
		LoadBalancing(RoundRobin(0, "web01", "web02", "web03")),
		Logging(log.New(os.Stdout, "client: ", log.LstdFlags)),
		Instrumentation(
			NewCounter("client.requests"),
			NewHistogram("client.latency", 0, 10e9, 3, 50, 90, 95, 99),
		),
		FaultTolerance(5, time.Second),
	)
}
