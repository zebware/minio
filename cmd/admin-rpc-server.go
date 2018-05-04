/*
 * Minio Cloud Storage, (C) 2016, 2017, 2018 Minio, Inc.
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
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"github.com/minio/minio/cmd/logger"
)

const adminPath = "/admin"

// adminCmd - exports RPC methods for service status, stop and
// restart commands.
type adminCmd struct {
	AuthRPCServer
}

// SignalServiceArgs - provides the signal argument to SignalService RPC
type SignalServiceArgs struct {
	AuthRPCArgs
	Sig serviceSignal
}

// ListLocksQuery - wraps ListLocks API's query values to send over RPC.
type ListLocksQuery struct {
	AuthRPCArgs
	Bucket   string
	Prefix   string
	Duration time.Duration
}

// ListLocksReply - wraps ListLocks response over RPC.
type ListLocksReply struct {
	AuthRPCReply
	VolLocks []VolumeLockInfo
}

// ServerInfoDataReply - wraps the server info response over RPC.
type ServerInfoDataReply struct {
	AuthRPCReply
	ServerInfoData ServerInfoData
}

// ConfigReply - wraps the server config response over RPC.
type ConfigReply struct {
	AuthRPCReply
	Config []byte // json-marshalled bytes of serverConfigV13
}

// SignalService - Send a restart or stop signal to the service
func (s *adminCmd) SignalService(args *SignalServiceArgs, reply *AuthRPCReply) error {
	if err := args.IsAuthenticated(); err != nil {
		return err
	}

	globalServiceSignalCh <- args.Sig
	return nil
}

// ReInitFormatArgs - provides dry-run information to re-initialize format.json
type ReInitFormatArgs struct {
	AuthRPCArgs
	DryRun bool
}

// ReInitFormat - re-init 'format.json'
func (s *adminCmd) ReInitFormat(args *ReInitFormatArgs, reply *AuthRPCReply) error {
	if err := args.IsAuthenticated(); err != nil {
		return err
	}
	objectAPI := newObjectLayerFn()
	if objectAPI == nil {
		return errServerNotInitialized
	}
	return objectAPI.ReloadFormat(context.Background(), args.DryRun)
}

// ListLocks - lists locks held by requests handled by this server instance.
func (s *adminCmd) ListLocks(query *ListLocksQuery, reply *ListLocksReply) error {
	if err := query.IsAuthenticated(); err != nil {
		return err
	}
	objectAPI := newObjectLayerFn()
	if objectAPI == nil {
		return errServerNotInitialized
	}
	volLocks, err := objectAPI.ListLocks(context.Background(), query.Bucket, query.Prefix, query.Duration)
	if err != nil {
		return err
	}
	*reply = ListLocksReply{VolLocks: volLocks}
	return nil
}

// ServerInfo - returns the server info when object layer was initialized on this server.
func (s *adminCmd) ServerInfoData(args *AuthRPCArgs, reply *ServerInfoDataReply) error {
	if err := args.IsAuthenticated(); err != nil {
		return err
	}

	if globalBootTime.IsZero() {
		return errServerNotInitialized
	}

	// Build storage info
	objLayer := newObjectLayerFn()
	if objLayer == nil {
		return errServerNotInitialized
	}
	storageInfo := objLayer.StorageInfo(context.Background())

	reply.ServerInfoData = ServerInfoData{
		Properties: ServerProperties{
			Uptime:   UTCNow().Sub(globalBootTime),
			Version:  Version,
			CommitID: CommitID,
			Region:   globalServerConfig.GetRegion(),
			SQSARN:   globalNotificationSys.GetARNList(),
		},
		StorageInfo: storageInfo,
		ConnStats:   globalConnStats.toServerConnStats(),
		HTTPStats:   globalHTTPStats.toServerHTTPStats(),
	}

	return nil
}

// GetConfig - returns the config.json of this server.
func (s *adminCmd) GetConfig(args *AuthRPCArgs, reply *ConfigReply) error {
	if err := args.IsAuthenticated(); err != nil {
		return err
	}

	if globalServerConfig == nil {
		return fmt.Errorf("config not present")
	}

	jsonBytes, err := json.Marshal(globalServerConfig)
	if err != nil {
		return err
	}

	reply.Config = jsonBytes
	return nil
}

// WriteConfigArgs - wraps the bytes to be written and temporary file name.
type WriteConfigArgs struct {
	AuthRPCArgs
	TmpFileName string
	Buf         []byte
}

// WriteConfigReply - wraps the result of a writing config into a temporary file.
// the remote node.
type WriteConfigReply struct {
	AuthRPCReply
}

func writeTmpConfigCommon(tmpFileName string, configBytes []byte) error {
	tmpConfigFile := filepath.Join(getConfigDir(), tmpFileName)
	err := ioutil.WriteFile(tmpConfigFile, configBytes, 0666)
	reqInfo := (&logger.ReqInfo{}).AppendTags("tmpConfigFile", tmpConfigFile)
	ctx := logger.SetReqInfo(context.Background(), reqInfo)
	logger.LogIf(ctx, err)
	return err
}

// WriteTmpConfig - writes the supplied config contents onto the
// supplied temporary file.
func (s *adminCmd) WriteTmpConfig(wArgs *WriteConfigArgs, wReply *WriteConfigReply) error {
	if err := wArgs.IsAuthenticated(); err != nil {
		return err
	}

	return writeTmpConfigCommon(wArgs.TmpFileName, wArgs.Buf)
}

// CommitConfigArgs - wraps the config file name that needs to be
// committed into config.json on this node.
type CommitConfigArgs struct {
	AuthRPCArgs
	FileName string
}

// CommitConfigReply - represents response to commit of config file on
// this node.
type CommitConfigReply struct {
	AuthRPCReply
}

// CommitConfig - Renames the temporary file into config.json on this node.
func (s *adminCmd) CommitConfig(cArgs *CommitConfigArgs, cReply *CommitConfigReply) error {
	configFile := getConfigFile()
	tmpConfigFile := filepath.Join(getConfigDir(), cArgs.FileName)

	err := os.Rename(tmpConfigFile, configFile)
	reqInfo := (&logger.ReqInfo{}).AppendTags("tmpConfigFile", tmpConfigFile)
	reqInfo.AppendTags("configFile", configFile)
	ctx := logger.SetReqInfo(context.Background(), reqInfo)
	logger.LogIf(ctx, err)
	return err
}

// registerAdminRPCRouter - registers RPC methods for service status,
// stop and restart commands.
func registerAdminRPCRouter(router *mux.Router) error {
	adminRPCHandler := &adminCmd{}
	adminRPCServer := newRPCServer()
	err := adminRPCServer.RegisterName("Admin", adminRPCHandler)
	if err != nil {
		logger.LogIf(context.Background(), err)
		return err
	}
	adminRouter := router.PathPrefix(minioReservedBucketPath).Subrouter()
	adminRouter.Path(adminPath).Handler(adminRPCServer)
	return nil
}
