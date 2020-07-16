// Code generated by go-swagger; DO NOT EDIT.

package hetzner

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
)

// New creates a new hetzner API client.
func New(transport runtime.ClientTransport, formats strfmt.Registry) ClientService {
	return &Client{transport: transport, formats: formats}
}

/*
Client for hetzner API
*/
type Client struct {
	transport runtime.ClientTransport
	formats   strfmt.Registry
}

// ClientService is the interface for Client methods
type ClientService interface {
	ListHetznerSizes(params *ListHetznerSizesParams, authInfo runtime.ClientAuthInfoWriter) (*ListHetznerSizesOK, error)

	ListHetznerSizesNoCredentials(params *ListHetznerSizesNoCredentialsParams, authInfo runtime.ClientAuthInfoWriter) (*ListHetznerSizesNoCredentialsOK, error)

	SetTransport(transport runtime.ClientTransport)
}

/*
  ListHetznerSizes Lists sizes from hetzner
*/
func (a *Client) ListHetznerSizes(params *ListHetznerSizesParams, authInfo runtime.ClientAuthInfoWriter) (*ListHetznerSizesOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewListHetznerSizesParams()
	}

	result, err := a.transport.Submit(&runtime.ClientOperation{
		ID:                 "listHetznerSizes",
		Method:             "GET",
		PathPattern:        "/api/v1/providers/hetzner/sizes",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &ListHetznerSizesReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	success, ok := result.(*ListHetznerSizesOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	unexpectedSuccess := result.(*ListHetznerSizesDefault)
	return nil, runtime.NewAPIError("unexpected success response: content available as default response in error", unexpectedSuccess, unexpectedSuccess.Code())
}

/*
  ListHetznerSizesNoCredentials Lists sizes from hetzner
*/
func (a *Client) ListHetznerSizesNoCredentials(params *ListHetznerSizesNoCredentialsParams, authInfo runtime.ClientAuthInfoWriter) (*ListHetznerSizesNoCredentialsOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewListHetznerSizesNoCredentialsParams()
	}

	result, err := a.transport.Submit(&runtime.ClientOperation{
		ID:                 "listHetznerSizesNoCredentials",
		Method:             "GET",
		PathPattern:        "/api/v1/projects/{project_id}/dc/{dc}/clusters/{cluster_id}/providers/hetzner/sizes",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &ListHetznerSizesNoCredentialsReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	success, ok := result.(*ListHetznerSizesNoCredentialsOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	unexpectedSuccess := result.(*ListHetznerSizesNoCredentialsDefault)
	return nil, runtime.NewAPIError("unexpected success response: content available as default response in error", unexpectedSuccess, unexpectedSuccess.Code())
}

// SetTransport changes the transport on the client
func (a *Client) SetTransport(transport runtime.ClientTransport) {
	a.transport = transport
}