/*
 * Minio Cloud Storage, (C) 2016, 2017 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"encoding/json"
	"os"
	"testing"
)

func testAdminCmd(cmd cmdType, t *testing.T) {
	// reset globals. this is to make sure that the tests are not
	// affected by modified globals.
	resetTestGlobals()

	rootPath, err := newTestConfig(globalMinioDefaultRegion)
	if err != nil {
		t.Fatalf("Failed to create test config - %v", err)
	}
	defer os.RemoveAll(rootPath)

	creds := globalServerConfig.GetCredential()
	token, err := authenticateNode(creds.AccessKey, creds.SecretKey)
	if err != nil {
		t.Fatal(err)
	}

	adminServer := adminCmd{}
	args := LoginRPCArgs{
		AuthToken:   token,
		Version:     globalRPCAPIVersion,
		RequestTime: UTCNow(),
	}
	err = adminServer.Login(&args, &LoginRPCReply{})
	if err != nil {
		t.Fatalf("Failed to login to admin server - %v", err)
	}

	go func() {
		// A test signal receiver
		<-globalServiceSignalCh
	}()

	sa := SignalServiceArgs{
		AuthRPCArgs: AuthRPCArgs{AuthToken: token, Version: globalRPCAPIVersion},
		Sig:         cmd.toServiceSignal(),
	}

	genReply := AuthRPCReply{}
	switch cmd {
	case restartCmd, stopCmd:
		if err = adminServer.SignalService(&sa, &genReply); err != nil {
			t.Errorf("restartCmd/stopCmd: Expected: <nil>, got: %v",
				err)
		}
	default:
		err = adminServer.SignalService(&sa, &genReply)
		if err != nil && err.Error() != errUnsupportedSignal.Error() {
			t.Errorf("invalidSignal %s: unexpected error got: %v",
				cmd, err)
		}
	}
}

// TestAdminRestart - test for Admin.Restart RPC service.
func TestAdminRestart(t *testing.T) {
	testAdminCmd(restartCmd, t)
}

// TestAdminStop - test for Admin.Stop RPC service.
func TestAdminStop(t *testing.T) {
	testAdminCmd(stopCmd, t)
}

// TestAdminStatus - test for Admin.Status RPC service (error case)
func TestAdminStatus(t *testing.T) {
	testAdminCmd(statusCmd, t)
}

// TestReInitFormat - test for Admin.ReInitFormat RPC service.
func TestReInitFormat(t *testing.T) {
	// Reset global variables to start afresh.
	resetTestGlobals()

	rootPath, err := newTestConfig("us-east-1")
	if err != nil {
		t.Fatalf("Unable to initialize server config. %s", err)
	}
	defer os.RemoveAll(rootPath)

	// Initializing objectLayer for HealFormatHandler.
	_, xlDirs, xlErr := initTestXLObjLayer()
	if xlErr != nil {
		t.Fatalf("failed to initialize XL based object layer - %v.", xlErr)
	}
	defer removeRoots(xlDirs)

	// Set globalEndpoints for a single node XL setup.
	globalEndpoints = mustGetNewEndpointList(xlDirs...)

	// Setup admin rpc server for an XL backend.
	globalIsXL = true
	adminServer := adminCmd{}

	creds := globalServerConfig.GetCredential()
	token, err := authenticateNode(creds.AccessKey, creds.SecretKey)
	if err != nil {
		t.Fatal(err)
	}

	args := LoginRPCArgs{
		AuthToken:   token,
		Version:     globalRPCAPIVersion,
		RequestTime: UTCNow(),
	}
	err = adminServer.Login(&args, &LoginRPCReply{})
	if err != nil {
		t.Fatalf("Failed to login to admin server - %v", err)
	}

	authArgs := AuthRPCArgs{
		AuthToken: token,
		Version:   globalRPCAPIVersion,
	}
	authReply := AuthRPCReply{}

	err = adminServer.ReInitFormat(&ReInitFormatArgs{
		AuthRPCArgs: authArgs,
		DryRun:      false,
	}, &authReply)
	if err != nil {
		t.Errorf("Expected to pass, but failed with %v", err)
	}
}

// TestGetConfig - Test for GetConfig admin RPC.
func TestGetConfig(t *testing.T) {
	// Reset global variables to start afresh.
	resetTestGlobals()

	rootPath, err := newTestConfig("us-east-1")
	if err != nil {
		t.Fatalf("Unable to initialize server config. %s", err)
	}
	defer os.RemoveAll(rootPath)

	adminServer := adminCmd{}
	creds := globalServerConfig.GetCredential()

	token, err := authenticateNode(creds.AccessKey, creds.SecretKey)
	if err != nil {
		t.Fatal(err)
	}

	args := LoginRPCArgs{
		AuthToken:   token,
		Version:     globalRPCAPIVersion,
		RequestTime: UTCNow(),
	}
	reply := LoginRPCReply{}
	err = adminServer.Login(&args, &reply)
	if err != nil {
		t.Fatalf("Failed to login to admin server - %v", err)
	}

	authArgs := AuthRPCArgs{
		AuthToken: token,
		Version:   globalRPCAPIVersion,
	}

	configReply := ConfigReply{}

	err = adminServer.GetConfig(&authArgs, &configReply)
	if err != nil {
		t.Errorf("Expected GetConfig to pass but failed with %v", err)
	}

	var config serverConfigV13
	err = json.Unmarshal(configReply.Config, &config)
	if err != nil {
		t.Errorf("Expected json unmarshal to pass but failed with %v", err)
	}
}

// TestWriteAndCommitConfig - test for WriteTmpConfig and CommitConfig
// RPC handler.
func TestWriteAndCommitConfig(t *testing.T) {
	// Reset global variables to start afresh.
	resetTestGlobals()

	rootPath, err := newTestConfig("us-east-1")
	if err != nil {
		t.Fatalf("Unable to initialize server config. %s", err)
	}
	defer os.RemoveAll(rootPath)

	adminServer := adminCmd{}
	creds := globalServerConfig.GetCredential()
	token, err := authenticateNode(creds.AccessKey, creds.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	args := LoginRPCArgs{
		AuthToken:   token,
		Version:     globalRPCAPIVersion,
		RequestTime: UTCNow(),
	}
	reply := LoginRPCReply{}
	err = adminServer.Login(&args, &reply)
	if err != nil {
		t.Fatalf("Failed to login to admin server - %v", err)
	}

	// Write temporary config.
	buf := []byte("hello")
	tmpFileName := mustGetUUID()
	wArgs := WriteConfigArgs{
		AuthRPCArgs: AuthRPCArgs{
			AuthToken: token,
			Version:   globalRPCAPIVersion,
		},
		TmpFileName: tmpFileName,
		Buf:         buf,
	}

	err = adminServer.WriteTmpConfig(&wArgs, &WriteConfigReply{})
	if err != nil {
		t.Fatalf("Failed to write temporary config %v", err)
	}

	if err != nil {
		t.Errorf("Expected to succeed but failed %v", err)
	}

	cArgs := CommitConfigArgs{
		AuthRPCArgs: AuthRPCArgs{
			AuthToken: token,
			Version:   globalRPCAPIVersion,
		},
		FileName: tmpFileName,
	}
	cReply := CommitConfigReply{}

	err = adminServer.CommitConfig(&cArgs, &cReply)
	if err != nil {
		t.Fatalf("Failed to commit config file %v", err)
	}
}
