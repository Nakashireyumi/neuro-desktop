module github.com/neuro-desktop/integration-code

go 1.22

require github.com/cassitly/neuro-integration-sdk v0.0.0-20260101084736-b81c7e4d4f4b

require github.com/gorilla/websocket v1.5.3 // indirect

replace github.com/cassitly/neuro-integration-sdk => ./third_party/neuro-integration-sdk
