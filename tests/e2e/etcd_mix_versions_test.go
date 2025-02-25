// Copyright 2022 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"go.etcd.io/etcd/client/pkg/v3/fileutil"
	"go.etcd.io/etcd/tests/v3/framework/config"
	"go.etcd.io/etcd/tests/v3/framework/e2e"
)

// TODO(ahrtr): add network partition scenario to trigger snapshots.
func TestMixVersionsSendSnapshot(t *testing.T) {
	cases := []struct {
		name              string
		clusterVersion    e2e.ClusterVersion
		newInstaceVersion e2e.ClusterVersion
	}{
		// etcd doesn't support adding a new member of old version into
		// a cluster with higher version. For example, etcd cluster
		// version is 3.6.x, then a new member of 3.5.x can't join the
		// cluster. Please refer to link below,
		// https://github.com/etcd-io/etcd/blob/3e903d0b12e399519a4013c52d4635ec8bdd6863/server/etcdserver/cluster_util.go#L222-L230
		/*{
			name:              "etcd instance with last version receives snapshot from the leader with current version",
			clusterVersion:    e2e.CurrentVersion,
			newInstaceVersion: e2e.LastVersion,
		},*/
		{
			name:              "etcd instance with current version receives snapshot from the leader with last version",
			clusterVersion:    e2e.LastVersion,
			newInstaceVersion: e2e.CurrentVersion,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mixVersionsSnapshotTest(t, tc.clusterVersion, tc.newInstaceVersion)
		})
	}
}

func mixVersionsSnapshotTest(t *testing.T, clusterVersion, newInstanceVersion e2e.ClusterVersion) {
	e2e.BeforeTest(t)

	if !fileutil.Exist(e2e.BinPath.EtcdLastRelease) {
		t.Skipf("%q does not exist", e2e.BinPath.EtcdLastRelease)
	}

	// Create an etcd cluster with 1 member
	cfg := &e2e.EtcdProcessClusterConfig{
		ClusterSize:   1,
		InitialToken:  "new",
		SnapshotCount: 10,
		Version:       clusterVersion,
	}

	epc, err := e2e.NewEtcdProcessCluster(context.TODO(), t, cfg)
	if err != nil {
		t.Fatalf("failed to start etcd cluster: %v", err)
	}
	defer func() {
		if err := epc.Close(); err != nil {
			t.Fatalf("failed to close etcd cluster: %v", err)
		}
	}()

	// Write more than SnapshotCount entries to trigger at least a snapshot.
	t.Log("Writing 20 keys to the cluster")
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := fmt.Sprintf("value-%d", i)
		if err := epc.Client().Put(context.TODO(), key, value, config.PutOptions{}); err != nil {
			t.Fatalf("failed to put %q, error: %v", key, err)
		}
	}

	// start a new etcd instance, which will receive a snapshot from the leader.
	newCfg := *epc.Cfg
	newCfg.Version = newInstanceVersion
	t.Log("Starting a new etcd instance")
	if err := epc.StartNewProc(context.TODO(), &newCfg, t); err != nil {
		t.Fatalf("failed to start the new etcd instance: %v", err)
	}
	defer epc.CloseProc(context.TODO(), nil)

	// verify all nodes have exact same revision and hash
	t.Log("Verify all nodes have exact same revision and hash")
	assert.Eventually(t, func() bool {
		hashKvs, err := epc.Client().HashKV(context.TODO(), 0)
		if err != nil {
			t.Logf("failed to get HashKV: %v", err)
			return false
		}
		if len(hashKvs) != 2 {
			t.Logf("expected 2 hashkv responses, but got: %d", len(hashKvs))
			return false
		}

		if hashKvs[0].Header.Revision != hashKvs[1].Header.Revision {
			t.Logf("Got different revisions, [%d, %d]", hashKvs[0].Header.Revision, hashKvs[1].Header.Revision)
			return false
		}

		assert.Equal(t, hashKvs[0].Hash, hashKvs[1].Hash)

		return true
	}, 10*time.Second, 500*time.Millisecond)
}
