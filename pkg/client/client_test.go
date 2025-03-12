package client

import (
	"context"
	"testing"
	"time"
)

func TestNewTransferClient(t *testing.T) {
	config := &Config{
		ServerID:   1,
		ServerName: "test-server",
		SessionID:  "test-session",
	}

	client := NewTransferClient("localhost:8002", config)
	if client == nil {
		t.Error("Failed to create new transfer client")
	}

	if client.serverAddr != "localhost:8002" {
		t.Errorf("Expected server address to be localhost:8002, got %s", client.serverAddr)
	}

	if client.config.ServerID != 1 {
		t.Errorf("Expected ServerID to be 1, got %d", client.config.ServerID)
	}

	if client.config.ServerName != "test-server" {
		t.Errorf("Expected ServerName to be test-server, got %s", client.config.ServerName)
	}

	if client.config.SessionID != "test-session" {
		t.Errorf("Expected SessionID to be test-session, got %s", client.config.SessionID)
	}
}

func TestTransferClient_Connect(t *testing.T) {
	config := &Config{
		ServerID:   1,
		ServerName: "test-server",
		SessionID:  "test-session",
	}

	client := NewTransferClient("localhost:8002", config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err == nil {
		// 如果没有运行服务器，应该会返回错误
		t.Error("Expected connection error when server is not running")
	}

	defer client.Close()
}

func TestTransferClient_SendInitRequest(t *testing.T) {
	config := &Config{
		ServerID:   1,
		ServerName: "test-server",
		SessionID:  "test-session",
	}

	client := NewTransferClient("localhost:8002", config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err == nil {
		err = client.SendInitRequest()
		if err == nil {
			t.Error("Expected init request error when server is not running")
		}
	}

	defer client.Close()
}

func TestTransferClient_SendTransferRequest(t *testing.T) {
	config := &Config{
		ServerID:   1,
		ServerName: "test-server",
		SessionID:  "test-session",
	}

	client := NewTransferClient("localhost:8002", config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err == nil {
		_, err = client.SendTransferRequest("test content")
		if err == nil {
			t.Error("Expected transfer request error when server is not running")
		}
	}

	defer client.Close()
}
