// Source: github.com/clustergarage/argus-proto/golang (interfaces: ArgusdClient,Argusd_GetWatchStateClient,Argusd_RecordMetricsClient)
package argus_mock

import (
	context "context"
	golang "github.com/clustergarage/argus-proto/golang"
	gomock "github.com/golang/mock/gomock"
	grpc "google.golang.org/grpc"
	metadata "google.golang.org/grpc/metadata"
	reflect "reflect"
)

// MockArgusdClient is a mock of ArgusdClient interface
type MockArgusdClient struct {
	ctrl     *gomock.Controller
	recorder *MockArgusdClientMockRecorder
}

// MockArgusdClientMockRecorder is the mock recorder for MockArgusdClient
type MockArgusdClientMockRecorder struct {
	mock *MockArgusdClient
}

// NewMockArgusdClient creates a new mock instance
func NewMockArgusdClient(ctrl *gomock.Controller) *MockArgusdClient {
	mock := &MockArgusdClient{ctrl: ctrl}
	mock.recorder = &MockArgusdClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockArgusdClient) EXPECT() *MockArgusdClientMockRecorder {
	return m.recorder
}

// CreateWatch mocks base method
func (m *MockArgusdClient) CreateWatch(arg0 context.Context, arg1 *golang.ArgusdConfig, arg2 ...grpc.CallOption) (*golang.ArgusdHandle, error) {
	varargs := []interface{}{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "CreateWatch", varargs...)
	ret0, _ := ret[0].(*golang.ArgusdHandle)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// CreateWatch indicates an expected call of CreateWatch
func (mr *MockArgusdClientMockRecorder) CreateWatch(arg0, arg1 interface{}, arg2 ...interface{}) *gomock.Call {
	varargs := append([]interface{}{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateWatch", reflect.TypeOf((*MockArgusdClient)(nil).CreateWatch), varargs...)
}

// DestroyWatch mocks base method
func (m *MockArgusdClient) DestroyWatch(arg0 context.Context, arg1 *golang.ArgusdConfig, arg2 ...grpc.CallOption) (*golang.Empty, error) {
	varargs := []interface{}{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "DestroyWatch", varargs...)
	ret0, _ := ret[0].(*golang.Empty)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DestroyWatch indicates an expected call of DestroyWatch
func (mr *MockArgusdClientMockRecorder) DestroyWatch(arg0, arg1 interface{}, arg2 ...interface{}) *gomock.Call {
	varargs := append([]interface{}{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DestroyWatch", reflect.TypeOf((*MockArgusdClient)(nil).DestroyWatch), varargs...)
}

// GetWatchState mocks base method
func (m *MockArgusdClient) GetWatchState(arg0 context.Context, arg1 *golang.Empty, arg2 ...grpc.CallOption) (golang.Argusd_GetWatchStateClient, error) {
	varargs := []interface{}{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "GetWatchState", varargs...)
	ret0, _ := ret[0].(golang.Argusd_GetWatchStateClient)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetWatchState indicates an expected call of GetWatchState
func (mr *MockArgusdClientMockRecorder) GetWatchState(arg0, arg1 interface{}, arg2 ...interface{}) *gomock.Call {
	varargs := append([]interface{}{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetWatchState", reflect.TypeOf((*MockArgusdClient)(nil).GetWatchState), varargs...)
}

// RecordMetrics mocks base method
func (m *MockArgusdClient) RecordMetrics(arg0 context.Context, arg1 *golang.Empty, arg2 ...grpc.CallOption) (golang.Argusd_RecordMetricsClient, error) {
	varargs := []interface{}{arg0, arg1}
	for _, a := range arg2 {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "RecordMetrics", varargs...)
	ret0, _ := ret[0].(golang.Argusd_RecordMetricsClient)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// RecordMetrics indicates an expected call of RecordMetrics
func (mr *MockArgusdClientMockRecorder) RecordMetrics(arg0, arg1 interface{}, arg2 ...interface{}) *gomock.Call {
	varargs := append([]interface{}{arg0, arg1}, arg2...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RecordMetrics", reflect.TypeOf((*MockArgusdClient)(nil).RecordMetrics), varargs...)
}

// MockArgusd_GetWatchStateClient is a mock of Argusd_GetWatchStateClient interface
type MockArgusd_GetWatchStateClient struct {
	ctrl     *gomock.Controller
	recorder *MockArgusd_GetWatchStateClientMockRecorder
}

// MockArgusd_GetWatchStateClientMockRecorder is the mock recorder for MockArgusd_GetWatchStateClient
type MockArgusd_GetWatchStateClientMockRecorder struct {
	mock *MockArgusd_GetWatchStateClient
}

// NewMockArgusd_GetWatchStateClient creates a new mock instance
func NewMockArgusd_GetWatchStateClient(ctrl *gomock.Controller) *MockArgusd_GetWatchStateClient {
	mock := &MockArgusd_GetWatchStateClient{ctrl: ctrl}
	mock.recorder = &MockArgusd_GetWatchStateClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockArgusd_GetWatchStateClient) EXPECT() *MockArgusd_GetWatchStateClientMockRecorder {
	return m.recorder
}

// CloseSend mocks base method
func (m *MockArgusd_GetWatchStateClient) CloseSend() error {
	ret := m.ctrl.Call(m, "CloseSend")
	ret0, _ := ret[0].(error)
	return ret0
}

// CloseSend indicates an expected call of CloseSend
func (mr *MockArgusd_GetWatchStateClientMockRecorder) CloseSend() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CloseSend", reflect.TypeOf((*MockArgusd_GetWatchStateClient)(nil).CloseSend))
}

// Context mocks base method
func (m *MockArgusd_GetWatchStateClient) Context() context.Context {
	ret := m.ctrl.Call(m, "Context")
	ret0, _ := ret[0].(context.Context)
	return ret0
}

// Context indicates an expected call of Context
func (mr *MockArgusd_GetWatchStateClientMockRecorder) Context() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Context", reflect.TypeOf((*MockArgusd_GetWatchStateClient)(nil).Context))
}

// Header mocks base method
func (m *MockArgusd_GetWatchStateClient) Header() (metadata.MD, error) {
	ret := m.ctrl.Call(m, "Header")
	ret0, _ := ret[0].(metadata.MD)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Header indicates an expected call of Header
func (mr *MockArgusd_GetWatchStateClientMockRecorder) Header() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Header", reflect.TypeOf((*MockArgusd_GetWatchStateClient)(nil).Header))
}

// Recv mocks base method
func (m *MockArgusd_GetWatchStateClient) Recv() (*golang.ArgusdHandle, error) {
	ret := m.ctrl.Call(m, "Recv")
	ret0, _ := ret[0].(*golang.ArgusdHandle)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Recv indicates an expected call of Recv
func (mr *MockArgusd_GetWatchStateClientMockRecorder) Recv() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Recv", reflect.TypeOf((*MockArgusd_GetWatchStateClient)(nil).Recv))
}

// RecvMsg mocks base method
func (m *MockArgusd_GetWatchStateClient) RecvMsg(arg0 interface{}) error {
	ret := m.ctrl.Call(m, "RecvMsg", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// RecvMsg indicates an expected call of RecvMsg
func (mr *MockArgusd_GetWatchStateClientMockRecorder) RecvMsg(arg0 interface{}) *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RecvMsg", reflect.TypeOf((*MockArgusd_GetWatchStateClient)(nil).RecvMsg), arg0)
}

// SendMsg mocks base method
func (m *MockArgusd_GetWatchStateClient) SendMsg(arg0 interface{}) error {
	ret := m.ctrl.Call(m, "SendMsg", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// SendMsg indicates an expected call of SendMsg
func (mr *MockArgusd_GetWatchStateClientMockRecorder) SendMsg(arg0 interface{}) *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendMsg", reflect.TypeOf((*MockArgusd_GetWatchStateClient)(nil).SendMsg), arg0)
}

// Trailer mocks base method
func (m *MockArgusd_GetWatchStateClient) Trailer() metadata.MD {
	ret := m.ctrl.Call(m, "Trailer")
	ret0, _ := ret[0].(metadata.MD)
	return ret0
}

// Trailer indicates an expected call of Trailer
func (mr *MockArgusd_GetWatchStateClientMockRecorder) Trailer() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Trailer", reflect.TypeOf((*MockArgusd_GetWatchStateClient)(nil).Trailer))
}

// MockArgusd_RecordMetricsClient is a mock of Argusd_RecordMetricsClient interface
type MockArgusd_RecordMetricsClient struct {
	ctrl     *gomock.Controller
	recorder *MockArgusd_RecordMetricsClientMockRecorder
}

// MockArgusd_RecordMetricsClientMockRecorder is the mock recorder for MockArgusd_RecordMetricsClient
type MockArgusd_RecordMetricsClientMockRecorder struct {
	mock *MockArgusd_RecordMetricsClient
}

// NewMockArgusd_RecordMetricsClient creates a new mock instance
func NewMockArgusd_RecordMetricsClient(ctrl *gomock.Controller) *MockArgusd_RecordMetricsClient {
	mock := &MockArgusd_RecordMetricsClient{ctrl: ctrl}
	mock.recorder = &MockArgusd_RecordMetricsClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockArgusd_RecordMetricsClient) EXPECT() *MockArgusd_RecordMetricsClientMockRecorder {
	return m.recorder
}

// CloseSend mocks base method
func (m *MockArgusd_RecordMetricsClient) CloseSend() error {
	ret := m.ctrl.Call(m, "CloseSend")
	ret0, _ := ret[0].(error)
	return ret0
}

// CloseSend indicates an expected call of CloseSend
func (mr *MockArgusd_RecordMetricsClientMockRecorder) CloseSend() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CloseSend", reflect.TypeOf((*MockArgusd_RecordMetricsClient)(nil).CloseSend))
}

// Context mocks base method
func (m *MockArgusd_RecordMetricsClient) Context() context.Context {
	ret := m.ctrl.Call(m, "Context")
	ret0, _ := ret[0].(context.Context)
	return ret0
}

// Context indicates an expected call of Context
func (mr *MockArgusd_RecordMetricsClientMockRecorder) Context() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Context", reflect.TypeOf((*MockArgusd_RecordMetricsClient)(nil).Context))
}

// Header mocks base method
func (m *MockArgusd_RecordMetricsClient) Header() (metadata.MD, error) {
	ret := m.ctrl.Call(m, "Header")
	ret0, _ := ret[0].(metadata.MD)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Header indicates an expected call of Header
func (mr *MockArgusd_RecordMetricsClientMockRecorder) Header() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Header", reflect.TypeOf((*MockArgusd_RecordMetricsClient)(nil).Header))
}

// Recv mocks base method
func (m *MockArgusd_RecordMetricsClient) Recv() (*golang.ArgusdMetricsHandle, error) {
	ret := m.ctrl.Call(m, "Recv")
	ret0, _ := ret[0].(*golang.ArgusdMetricsHandle)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Recv indicates an expected call of Recv
func (mr *MockArgusd_RecordMetricsClientMockRecorder) Recv() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Recv", reflect.TypeOf((*MockArgusd_RecordMetricsClient)(nil).Recv))
}

// RecvMsg mocks base method
func (m *MockArgusd_RecordMetricsClient) RecvMsg(arg0 interface{}) error {
	ret := m.ctrl.Call(m, "RecvMsg", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// RecvMsg indicates an expected call of RecvMsg
func (mr *MockArgusd_RecordMetricsClientMockRecorder) RecvMsg(arg0 interface{}) *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RecvMsg", reflect.TypeOf((*MockArgusd_RecordMetricsClient)(nil).RecvMsg), arg0)
}

// SendMsg mocks base method
func (m *MockArgusd_RecordMetricsClient) SendMsg(arg0 interface{}) error {
	ret := m.ctrl.Call(m, "SendMsg", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// SendMsg indicates an expected call of SendMsg
func (mr *MockArgusd_RecordMetricsClientMockRecorder) SendMsg(arg0 interface{}) *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendMsg", reflect.TypeOf((*MockArgusd_RecordMetricsClient)(nil).SendMsg), arg0)
}

// Trailer mocks base method
func (m *MockArgusd_RecordMetricsClient) Trailer() metadata.MD {
	ret := m.ctrl.Call(m, "Trailer")
	ret0, _ := ret[0].(metadata.MD)
	return ret0
}

// Trailer indicates an expected call of Trailer
func (mr *MockArgusd_RecordMetricsClientMockRecorder) Trailer() *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Trailer", reflect.TypeOf((*MockArgusd_RecordMetricsClient)(nil).Trailer))
}
