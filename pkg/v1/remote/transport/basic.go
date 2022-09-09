// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transport

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/logs"
)

type basicTransport struct {
	inner  http.RoundTripper
	auth   authn.Authenticator
	target string
}

var _ http.RoundTripper = (*basicTransport)(nil)

// RoundTrip implements http.RoundTripper
func (bt *basicTransport) RoundTrip(in *http.Request) (*http.Response, error) {
	// If the Authenticator is a MultiAuthenticator, we get all the auths it has and try them in order until one works.
	// If there's only one auth, we just use that.
	var auths []authn.AuthConfig
	if ma, ok := bt.auth.(authn.MultiAuthenticator); ok {
		var err error
		auths, err = ma.Authorizations()
		if err != nil {
			return nil, err
		}
	} else {
		auth, err := bt.auth.Authorization()
		if err != nil {
			return nil, err
		}
		auths = []authn.AuthConfig{*auth}
	}

	for idx, auth := range auths {
		// http.Client handles redirects at a layer above the http.RoundTripper
		// abstraction, so to avoid forwarding Authorization headers to places
		// we are redirected, only set it when the authorization header matches
		// the host with which we are interacting.
		// In case of redirect http.Client can use an empty Host, check URL too.
		if in.Host == bt.target || in.URL.Host == bt.target {
			if bearer := auth.RegistryToken; bearer != "" {
				hdr := fmt.Sprintf("Bearer %s", bearer)
				in.Header.Set("Authorization", hdr)
			} else if user, pass := auth.Username, auth.Password; user != "" && pass != "" {
				delimited := fmt.Sprintf("%s:%s", user, pass)
				encoded := base64.StdEncoding.EncodeToString([]byte(delimited))
				hdr := fmt.Sprintf("Basic %s", encoded)
				in.Header.Set("Authorization", hdr)
			} else if token := auth.Auth; token != "" {
				hdr := fmt.Sprintf("Basic %s", token)
				in.Header.Set("Authorization", hdr)
			}
		}
		resp, err := bt.inner.RoundTrip(in)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			if idx == len(auths)-1 {
				return resp, nil
			}
			respBody, _ := ioutil.ReadAll(resp.Body)
			logs.Debug.Printf("Basic Transport check error, the response is:%s", string(respBody))
			continue
		} else {
			return resp, nil
		}
	}
	panic("unreachable")
}
