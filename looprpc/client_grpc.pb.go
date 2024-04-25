// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package looprpc

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// SwapClientClient is the client API for SwapClient service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type SwapClientClient interface {
	// loop: `out`
	// LoopOut initiates an loop out swap with the given parameters. The call
	// returns after the swap has been set up with the swap server. From that
	// point onwards, progress can be tracked via the SwapStatus stream that is
	// returned from Monitor().
	LoopOut(ctx context.Context, in *LoopOutRequest, opts ...grpc.CallOption) (*SwapResponse, error)
	// loop: `in`
	// LoopIn initiates a loop in swap with the given parameters. The call
	// returns after the swap has been set up with the swap server. From that
	// point onwards, progress can be tracked via the SwapStatus stream
	// that is returned from Monitor().
	LoopIn(ctx context.Context, in *LoopInRequest, opts ...grpc.CallOption) (*SwapResponse, error)
	// loop: `monitor`
	// Monitor will return a stream of swap updates for currently active swaps.
	Monitor(ctx context.Context, in *MonitorRequest, opts ...grpc.CallOption) (SwapClient_MonitorClient, error)
	// loop: `listswaps`
	// ListSwaps returns a list of all currently known swaps and their current
	// status.
	ListSwaps(ctx context.Context, in *ListSwapsRequest, opts ...grpc.CallOption) (*ListSwapsResponse, error)
	// loop: `swapinfo`
	// SwapInfo returns all known details about a single swap.
	SwapInfo(ctx context.Context, in *SwapInfoRequest, opts ...grpc.CallOption) (*SwapStatus, error)
	// loop: `abandonswap`
	// AbandonSwap allows the client to abandon a swap.
	AbandonSwap(ctx context.Context, in *AbandonSwapRequest, opts ...grpc.CallOption) (*AbandonSwapResponse, error)
	// loop: `terms`
	// LoopOutTerms returns the terms that the server enforces for a loop out swap.
	LoopOutTerms(ctx context.Context, in *TermsRequest, opts ...grpc.CallOption) (*OutTermsResponse, error)
	// loop: `quote`
	// LoopOutQuote returns a quote for a loop out swap with the provided
	// parameters.
	LoopOutQuote(ctx context.Context, in *QuoteRequest, opts ...grpc.CallOption) (*OutQuoteResponse, error)
	// loop: `terms`
	// GetTerms returns the terms that the server enforces for swaps.
	GetLoopInTerms(ctx context.Context, in *TermsRequest, opts ...grpc.CallOption) (*InTermsResponse, error)
	// loop: `quote`
	// GetQuote returns a quote for a swap with the provided parameters.
	GetLoopInQuote(ctx context.Context, in *QuoteRequest, opts ...grpc.CallOption) (*InQuoteResponse, error)
	// Probe asks he sever to probe the route to us to have a better upfront
	// estimate about routing fees when loopin-in.
	Probe(ctx context.Context, in *ProbeRequest, opts ...grpc.CallOption) (*ProbeResponse, error)
	// loop: `listauth`
	// GetL402Tokens returns all L402 tokens the daemon ever paid for.
	GetL402Tokens(ctx context.Context, in *TokensRequest, opts ...grpc.CallOption) (*TokensResponse, error)
	// loop: `listauth`
	// Deprecated: use GetL402Tokens.
	// This API is provided to maintain backward compatibility with gRPC clients
	// (e.g. `loop listauth`, Terminal Web, RTL).
	// Type LsatToken used by GetLsatTokens in the past was renamed to L402Token,
	// but this does not affect binary encoding, so we can use type L402Token here.
	GetLsatTokens(ctx context.Context, in *TokensRequest, opts ...grpc.CallOption) (*TokensResponse, error)
	// loop: `getinfo`
	// GetInfo gets basic information about the loop daemon.
	GetInfo(ctx context.Context, in *GetInfoRequest, opts ...grpc.CallOption) (*GetInfoResponse, error)
	// loop: `getparams`
	// GetLiquidityParams gets the parameters that the daemon's liquidity manager
	// is currently configured with. This may be nil if nothing is configured.
	// [EXPERIMENTAL]: endpoint is subject to change.
	GetLiquidityParams(ctx context.Context, in *GetLiquidityParamsRequest, opts ...grpc.CallOption) (*LiquidityParameters, error)
	// loop: `setparams`
	// SetLiquidityParams sets a new set of parameters for the daemon's liquidity
	// manager. Note that the full set of parameters must be provided, because
	// this call fully overwrites our existing parameters.
	// [EXPERIMENTAL]: endpoint is subject to change.
	SetLiquidityParams(ctx context.Context, in *SetLiquidityParamsRequest, opts ...grpc.CallOption) (*SetLiquidityParamsResponse, error)
	// loop: `suggestswaps`
	// SuggestSwaps returns a list of recommended swaps based on the current
	// state of your node's channels and it's liquidity manager parameters.
	// Note that only loop out suggestions are currently supported.
	// [EXPERIMENTAL]: endpoint is subject to change.
	SuggestSwaps(ctx context.Context, in *SuggestSwapsRequest, opts ...grpc.CallOption) (*SuggestSwapsResponse, error)
	// loop: `listreservations`
	// ListReservations returns a list of all reservations the server opened to us.
	ListReservations(ctx context.Context, in *ListReservationsRequest, opts ...grpc.CallOption) (*ListReservationsResponse, error)
	// loop: `instantout`
	// InstantOut initiates an instant out swap with the given parameters.
	InstantOut(ctx context.Context, in *InstantOutRequest, opts ...grpc.CallOption) (*InstantOutResponse, error)
	// loop: `instantoutquote`
	// InstantOutQuote returns a quote for an instant out swap with the provided
	// parameters.
	InstantOutQuote(ctx context.Context, in *InstantOutQuoteRequest, opts ...grpc.CallOption) (*InstantOutQuoteResponse, error)
	// loop: `listinstantouts`
	// ListInstantOuts returns a list of all currently known instant out swaps and
	// their current status.
	ListInstantOuts(ctx context.Context, in *ListInstantOutsRequest, opts ...grpc.CallOption) (*ListInstantOutsResponse, error)
}

type swapClientClient struct {
	cc grpc.ClientConnInterface
}

func NewSwapClientClient(cc grpc.ClientConnInterface) SwapClientClient {
	return &swapClientClient{cc}
}

func (c *swapClientClient) LoopOut(ctx context.Context, in *LoopOutRequest, opts ...grpc.CallOption) (*SwapResponse, error) {
	out := new(SwapResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/LoopOut", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) LoopIn(ctx context.Context, in *LoopInRequest, opts ...grpc.CallOption) (*SwapResponse, error) {
	out := new(SwapResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/LoopIn", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) Monitor(ctx context.Context, in *MonitorRequest, opts ...grpc.CallOption) (SwapClient_MonitorClient, error) {
	stream, err := c.cc.NewStream(ctx, &SwapClient_ServiceDesc.Streams[0], "/looprpc.SwapClient/Monitor", opts...)
	if err != nil {
		return nil, err
	}
	x := &swapClientMonitorClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type SwapClient_MonitorClient interface {
	Recv() (*SwapStatus, error)
	grpc.ClientStream
}

type swapClientMonitorClient struct {
	grpc.ClientStream
}

func (x *swapClientMonitorClient) Recv() (*SwapStatus, error) {
	m := new(SwapStatus)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *swapClientClient) ListSwaps(ctx context.Context, in *ListSwapsRequest, opts ...grpc.CallOption) (*ListSwapsResponse, error) {
	out := new(ListSwapsResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/ListSwaps", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) SwapInfo(ctx context.Context, in *SwapInfoRequest, opts ...grpc.CallOption) (*SwapStatus, error) {
	out := new(SwapStatus)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/SwapInfo", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) AbandonSwap(ctx context.Context, in *AbandonSwapRequest, opts ...grpc.CallOption) (*AbandonSwapResponse, error) {
	out := new(AbandonSwapResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/AbandonSwap", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) LoopOutTerms(ctx context.Context, in *TermsRequest, opts ...grpc.CallOption) (*OutTermsResponse, error) {
	out := new(OutTermsResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/LoopOutTerms", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) LoopOutQuote(ctx context.Context, in *QuoteRequest, opts ...grpc.CallOption) (*OutQuoteResponse, error) {
	out := new(OutQuoteResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/LoopOutQuote", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) GetLoopInTerms(ctx context.Context, in *TermsRequest, opts ...grpc.CallOption) (*InTermsResponse, error) {
	out := new(InTermsResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/GetLoopInTerms", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) GetLoopInQuote(ctx context.Context, in *QuoteRequest, opts ...grpc.CallOption) (*InQuoteResponse, error) {
	out := new(InQuoteResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/GetLoopInQuote", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) Probe(ctx context.Context, in *ProbeRequest, opts ...grpc.CallOption) (*ProbeResponse, error) {
	out := new(ProbeResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/Probe", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) GetL402Tokens(ctx context.Context, in *TokensRequest, opts ...grpc.CallOption) (*TokensResponse, error) {
	out := new(TokensResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/GetL402Tokens", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) GetLsatTokens(ctx context.Context, in *TokensRequest, opts ...grpc.CallOption) (*TokensResponse, error) {
	out := new(TokensResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/GetLsatTokens", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) GetInfo(ctx context.Context, in *GetInfoRequest, opts ...grpc.CallOption) (*GetInfoResponse, error) {
	out := new(GetInfoResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/GetInfo", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) GetLiquidityParams(ctx context.Context, in *GetLiquidityParamsRequest, opts ...grpc.CallOption) (*LiquidityParameters, error) {
	out := new(LiquidityParameters)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/GetLiquidityParams", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) SetLiquidityParams(ctx context.Context, in *SetLiquidityParamsRequest, opts ...grpc.CallOption) (*SetLiquidityParamsResponse, error) {
	out := new(SetLiquidityParamsResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/SetLiquidityParams", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) SuggestSwaps(ctx context.Context, in *SuggestSwapsRequest, opts ...grpc.CallOption) (*SuggestSwapsResponse, error) {
	out := new(SuggestSwapsResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/SuggestSwaps", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) ListReservations(ctx context.Context, in *ListReservationsRequest, opts ...grpc.CallOption) (*ListReservationsResponse, error) {
	out := new(ListReservationsResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/ListReservations", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) InstantOut(ctx context.Context, in *InstantOutRequest, opts ...grpc.CallOption) (*InstantOutResponse, error) {
	out := new(InstantOutResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/InstantOut", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) InstantOutQuote(ctx context.Context, in *InstantOutQuoteRequest, opts ...grpc.CallOption) (*InstantOutQuoteResponse, error) {
	out := new(InstantOutQuoteResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/InstantOutQuote", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *swapClientClient) ListInstantOuts(ctx context.Context, in *ListInstantOutsRequest, opts ...grpc.CallOption) (*ListInstantOutsResponse, error) {
	out := new(ListInstantOutsResponse)
	err := c.cc.Invoke(ctx, "/looprpc.SwapClient/ListInstantOuts", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SwapClientServer is the server API for SwapClient service.
// All implementations must embed UnimplementedSwapClientServer
// for forward compatibility
type SwapClientServer interface {
	// loop: `out`
	// LoopOut initiates an loop out swap with the given parameters. The call
	// returns after the swap has been set up with the swap server. From that
	// point onwards, progress can be tracked via the SwapStatus stream that is
	// returned from Monitor().
	LoopOut(context.Context, *LoopOutRequest) (*SwapResponse, error)
	// loop: `in`
	// LoopIn initiates a loop in swap with the given parameters. The call
	// returns after the swap has been set up with the swap server. From that
	// point onwards, progress can be tracked via the SwapStatus stream
	// that is returned from Monitor().
	LoopIn(context.Context, *LoopInRequest) (*SwapResponse, error)
	// loop: `monitor`
	// Monitor will return a stream of swap updates for currently active swaps.
	Monitor(*MonitorRequest, SwapClient_MonitorServer) error
	// loop: `listswaps`
	// ListSwaps returns a list of all currently known swaps and their current
	// status.
	ListSwaps(context.Context, *ListSwapsRequest) (*ListSwapsResponse, error)
	// loop: `swapinfo`
	// SwapInfo returns all known details about a single swap.
	SwapInfo(context.Context, *SwapInfoRequest) (*SwapStatus, error)
	// loop: `abandonswap`
	// AbandonSwap allows the client to abandon a swap.
	AbandonSwap(context.Context, *AbandonSwapRequest) (*AbandonSwapResponse, error)
	// loop: `terms`
	// LoopOutTerms returns the terms that the server enforces for a loop out swap.
	LoopOutTerms(context.Context, *TermsRequest) (*OutTermsResponse, error)
	// loop: `quote`
	// LoopOutQuote returns a quote for a loop out swap with the provided
	// parameters.
	LoopOutQuote(context.Context, *QuoteRequest) (*OutQuoteResponse, error)
	// loop: `terms`
	// GetTerms returns the terms that the server enforces for swaps.
	GetLoopInTerms(context.Context, *TermsRequest) (*InTermsResponse, error)
	// loop: `quote`
	// GetQuote returns a quote for a swap with the provided parameters.
	GetLoopInQuote(context.Context, *QuoteRequest) (*InQuoteResponse, error)
	// Probe asks he sever to probe the route to us to have a better upfront
	// estimate about routing fees when loopin-in.
	Probe(context.Context, *ProbeRequest) (*ProbeResponse, error)
	// loop: `listauth`
	// GetL402Tokens returns all L402 tokens the daemon ever paid for.
	GetL402Tokens(context.Context, *TokensRequest) (*TokensResponse, error)
	// loop: `listauth`
	// Deprecated: use GetL402Tokens.
	// This API is provided to maintain backward compatibility with gRPC clients
	// (e.g. `loop listauth`, Terminal Web, RTL).
	// Type LsatToken used by GetLsatTokens in the past was renamed to L402Token,
	// but this does not affect binary encoding, so we can use type L402Token here.
	GetLsatTokens(context.Context, *TokensRequest) (*TokensResponse, error)
	// loop: `getinfo`
	// GetInfo gets basic information about the loop daemon.
	GetInfo(context.Context, *GetInfoRequest) (*GetInfoResponse, error)
	// loop: `getparams`
	// GetLiquidityParams gets the parameters that the daemon's liquidity manager
	// is currently configured with. This may be nil if nothing is configured.
	// [EXPERIMENTAL]: endpoint is subject to change.
	GetLiquidityParams(context.Context, *GetLiquidityParamsRequest) (*LiquidityParameters, error)
	// loop: `setparams`
	// SetLiquidityParams sets a new set of parameters for the daemon's liquidity
	// manager. Note that the full set of parameters must be provided, because
	// this call fully overwrites our existing parameters.
	// [EXPERIMENTAL]: endpoint is subject to change.
	SetLiquidityParams(context.Context, *SetLiquidityParamsRequest) (*SetLiquidityParamsResponse, error)
	// loop: `suggestswaps`
	// SuggestSwaps returns a list of recommended swaps based on the current
	// state of your node's channels and it's liquidity manager parameters.
	// Note that only loop out suggestions are currently supported.
	// [EXPERIMENTAL]: endpoint is subject to change.
	SuggestSwaps(context.Context, *SuggestSwapsRequest) (*SuggestSwapsResponse, error)
	// loop: `listreservations`
	// ListReservations returns a list of all reservations the server opened to us.
	ListReservations(context.Context, *ListReservationsRequest) (*ListReservationsResponse, error)
	// loop: `instantout`
	// InstantOut initiates an instant out swap with the given parameters.
	InstantOut(context.Context, *InstantOutRequest) (*InstantOutResponse, error)
	// loop: `instantoutquote`
	// InstantOutQuote returns a quote for an instant out swap with the provided
	// parameters.
	InstantOutQuote(context.Context, *InstantOutQuoteRequest) (*InstantOutQuoteResponse, error)
	// loop: `listinstantouts`
	// ListInstantOuts returns a list of all currently known instant out swaps and
	// their current status.
	ListInstantOuts(context.Context, *ListInstantOutsRequest) (*ListInstantOutsResponse, error)
	mustEmbedUnimplementedSwapClientServer()
}

// UnimplementedSwapClientServer must be embedded to have forward compatible implementations.
type UnimplementedSwapClientServer struct {
}

func (UnimplementedSwapClientServer) LoopOut(context.Context, *LoopOutRequest) (*SwapResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method LoopOut not implemented")
}
func (UnimplementedSwapClientServer) LoopIn(context.Context, *LoopInRequest) (*SwapResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method LoopIn not implemented")
}
func (UnimplementedSwapClientServer) Monitor(*MonitorRequest, SwapClient_MonitorServer) error {
	return status.Errorf(codes.Unimplemented, "method Monitor not implemented")
}
func (UnimplementedSwapClientServer) ListSwaps(context.Context, *ListSwapsRequest) (*ListSwapsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListSwaps not implemented")
}
func (UnimplementedSwapClientServer) SwapInfo(context.Context, *SwapInfoRequest) (*SwapStatus, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SwapInfo not implemented")
}
func (UnimplementedSwapClientServer) AbandonSwap(context.Context, *AbandonSwapRequest) (*AbandonSwapResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method AbandonSwap not implemented")
}
func (UnimplementedSwapClientServer) LoopOutTerms(context.Context, *TermsRequest) (*OutTermsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method LoopOutTerms not implemented")
}
func (UnimplementedSwapClientServer) LoopOutQuote(context.Context, *QuoteRequest) (*OutQuoteResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method LoopOutQuote not implemented")
}
func (UnimplementedSwapClientServer) GetLoopInTerms(context.Context, *TermsRequest) (*InTermsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetLoopInTerms not implemented")
}
func (UnimplementedSwapClientServer) GetLoopInQuote(context.Context, *QuoteRequest) (*InQuoteResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetLoopInQuote not implemented")
}
func (UnimplementedSwapClientServer) Probe(context.Context, *ProbeRequest) (*ProbeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Probe not implemented")
}
func (UnimplementedSwapClientServer) GetL402Tokens(context.Context, *TokensRequest) (*TokensResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetL402Tokens not implemented")
}
func (UnimplementedSwapClientServer) GetLsatTokens(context.Context, *TokensRequest) (*TokensResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetLsatTokens not implemented")
}
func (UnimplementedSwapClientServer) GetInfo(context.Context, *GetInfoRequest) (*GetInfoResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetInfo not implemented")
}
func (UnimplementedSwapClientServer) GetLiquidityParams(context.Context, *GetLiquidityParamsRequest) (*LiquidityParameters, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetLiquidityParams not implemented")
}
func (UnimplementedSwapClientServer) SetLiquidityParams(context.Context, *SetLiquidityParamsRequest) (*SetLiquidityParamsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SetLiquidityParams not implemented")
}
func (UnimplementedSwapClientServer) SuggestSwaps(context.Context, *SuggestSwapsRequest) (*SuggestSwapsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SuggestSwaps not implemented")
}
func (UnimplementedSwapClientServer) ListReservations(context.Context, *ListReservationsRequest) (*ListReservationsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListReservations not implemented")
}
func (UnimplementedSwapClientServer) InstantOut(context.Context, *InstantOutRequest) (*InstantOutResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method InstantOut not implemented")
}
func (UnimplementedSwapClientServer) InstantOutQuote(context.Context, *InstantOutQuoteRequest) (*InstantOutQuoteResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method InstantOutQuote not implemented")
}
func (UnimplementedSwapClientServer) ListInstantOuts(context.Context, *ListInstantOutsRequest) (*ListInstantOutsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListInstantOuts not implemented")
}
func (UnimplementedSwapClientServer) mustEmbedUnimplementedSwapClientServer() {}

// UnsafeSwapClientServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to SwapClientServer will
// result in compilation errors.
type UnsafeSwapClientServer interface {
	mustEmbedUnimplementedSwapClientServer()
}

func RegisterSwapClientServer(s grpc.ServiceRegistrar, srv SwapClientServer) {
	s.RegisterService(&SwapClient_ServiceDesc, srv)
}

func _SwapClient_LoopOut_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(LoopOutRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).LoopOut(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/LoopOut",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).LoopOut(ctx, req.(*LoopOutRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_LoopIn_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(LoopInRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).LoopIn(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/LoopIn",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).LoopIn(ctx, req.(*LoopInRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_Monitor_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(MonitorRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(SwapClientServer).Monitor(m, &swapClientMonitorServer{stream})
}

type SwapClient_MonitorServer interface {
	Send(*SwapStatus) error
	grpc.ServerStream
}

type swapClientMonitorServer struct {
	grpc.ServerStream
}

func (x *swapClientMonitorServer) Send(m *SwapStatus) error {
	return x.ServerStream.SendMsg(m)
}

func _SwapClient_ListSwaps_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListSwapsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).ListSwaps(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/ListSwaps",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).ListSwaps(ctx, req.(*ListSwapsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_SwapInfo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SwapInfoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).SwapInfo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/SwapInfo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).SwapInfo(ctx, req.(*SwapInfoRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_AbandonSwap_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AbandonSwapRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).AbandonSwap(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/AbandonSwap",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).AbandonSwap(ctx, req.(*AbandonSwapRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_LoopOutTerms_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(TermsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).LoopOutTerms(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/LoopOutTerms",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).LoopOutTerms(ctx, req.(*TermsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_LoopOutQuote_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(QuoteRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).LoopOutQuote(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/LoopOutQuote",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).LoopOutQuote(ctx, req.(*QuoteRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_GetLoopInTerms_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(TermsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).GetLoopInTerms(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/GetLoopInTerms",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).GetLoopInTerms(ctx, req.(*TermsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_GetLoopInQuote_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(QuoteRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).GetLoopInQuote(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/GetLoopInQuote",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).GetLoopInQuote(ctx, req.(*QuoteRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_Probe_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ProbeRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).Probe(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/Probe",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).Probe(ctx, req.(*ProbeRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_GetL402Tokens_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(TokensRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).GetL402Tokens(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/GetL402Tokens",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).GetL402Tokens(ctx, req.(*TokensRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_GetLsatTokens_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(TokensRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).GetLsatTokens(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/GetLsatTokens",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).GetLsatTokens(ctx, req.(*TokensRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_GetInfo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetInfoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).GetInfo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/GetInfo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).GetInfo(ctx, req.(*GetInfoRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_GetLiquidityParams_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetLiquidityParamsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).GetLiquidityParams(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/GetLiquidityParams",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).GetLiquidityParams(ctx, req.(*GetLiquidityParamsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_SetLiquidityParams_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SetLiquidityParamsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).SetLiquidityParams(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/SetLiquidityParams",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).SetLiquidityParams(ctx, req.(*SetLiquidityParamsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_SuggestSwaps_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SuggestSwapsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).SuggestSwaps(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/SuggestSwaps",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).SuggestSwaps(ctx, req.(*SuggestSwapsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_ListReservations_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListReservationsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).ListReservations(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/ListReservations",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).ListReservations(ctx, req.(*ListReservationsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_InstantOut_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(InstantOutRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).InstantOut(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/InstantOut",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).InstantOut(ctx, req.(*InstantOutRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_InstantOutQuote_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(InstantOutQuoteRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).InstantOutQuote(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/InstantOutQuote",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).InstantOutQuote(ctx, req.(*InstantOutQuoteRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SwapClient_ListInstantOuts_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListInstantOutsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SwapClientServer).ListInstantOuts(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/looprpc.SwapClient/ListInstantOuts",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SwapClientServer).ListInstantOuts(ctx, req.(*ListInstantOutsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// SwapClient_ServiceDesc is the grpc.ServiceDesc for SwapClient service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var SwapClient_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "looprpc.SwapClient",
	HandlerType: (*SwapClientServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "LoopOut",
			Handler:    _SwapClient_LoopOut_Handler,
		},
		{
			MethodName: "LoopIn",
			Handler:    _SwapClient_LoopIn_Handler,
		},
		{
			MethodName: "ListSwaps",
			Handler:    _SwapClient_ListSwaps_Handler,
		},
		{
			MethodName: "SwapInfo",
			Handler:    _SwapClient_SwapInfo_Handler,
		},
		{
			MethodName: "AbandonSwap",
			Handler:    _SwapClient_AbandonSwap_Handler,
		},
		{
			MethodName: "LoopOutTerms",
			Handler:    _SwapClient_LoopOutTerms_Handler,
		},
		{
			MethodName: "LoopOutQuote",
			Handler:    _SwapClient_LoopOutQuote_Handler,
		},
		{
			MethodName: "GetLoopInTerms",
			Handler:    _SwapClient_GetLoopInTerms_Handler,
		},
		{
			MethodName: "GetLoopInQuote",
			Handler:    _SwapClient_GetLoopInQuote_Handler,
		},
		{
			MethodName: "Probe",
			Handler:    _SwapClient_Probe_Handler,
		},
		{
			MethodName: "GetL402Tokens",
			Handler:    _SwapClient_GetL402Tokens_Handler,
		},
		{
			MethodName: "GetLsatTokens",
			Handler:    _SwapClient_GetLsatTokens_Handler,
		},
		{
			MethodName: "GetInfo",
			Handler:    _SwapClient_GetInfo_Handler,
		},
		{
			MethodName: "GetLiquidityParams",
			Handler:    _SwapClient_GetLiquidityParams_Handler,
		},
		{
			MethodName: "SetLiquidityParams",
			Handler:    _SwapClient_SetLiquidityParams_Handler,
		},
		{
			MethodName: "SuggestSwaps",
			Handler:    _SwapClient_SuggestSwaps_Handler,
		},
		{
			MethodName: "ListReservations",
			Handler:    _SwapClient_ListReservations_Handler,
		},
		{
			MethodName: "InstantOut",
			Handler:    _SwapClient_InstantOut_Handler,
		},
		{
			MethodName: "InstantOutQuote",
			Handler:    _SwapClient_InstantOutQuote_Handler,
		},
		{
			MethodName: "ListInstantOuts",
			Handler:    _SwapClient_ListInstantOuts_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Monitor",
			Handler:       _SwapClient_Monitor_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "client.proto",
}
