/*
 * Copyright (c) 2018, NVIDIA CORPORATION. All rights reserved.
 *
 */
package dfc_test

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/NVIDIA/dfcpub/api"
	"github.com/NVIDIA/dfcpub/cmn"
	"github.com/NVIDIA/dfcpub/tutils"
)

func testBucketProps(t *testing.T) *cmn.BucketProps {
	proxyURL := getPrimaryURL(t, proxyURLReadOnly)
	globalConfig := getDaemonConfig(t, proxyURL)

	return &cmn.BucketProps{
		CksumConf: cmn.CksumConf{Checksum: cmn.ChecksumInherit},
		LRUConf:   globalConfig.LRU,
	}
}

func TestResetBucketProps(t *testing.T) {
	var (
		proxyURL     = getPrimaryURL(t, proxyURLReadOnly)
		globalProps  cmn.BucketProps
		globalConfig = getDaemonConfig(t, proxyURL)
	)

	tutils.CreateFreshLocalBucket(t, proxyURL, TestLocalBucketName)
	defer tutils.DestroyLocalBucket(t, proxyURL, TestLocalBucketName)

	bucketProps := defaultBucketProps()
	bucketProps.Checksum = cmn.ChecksumNone
	bucketProps.ValidateWarmGet = true
	bucketProps.EnableReadRangeChecksum = true

	globalProps.CloudProvider = cmn.ProviderDFC
	globalProps.CksumConf = globalConfig.Cksum
	globalProps.LRUConf = testBucketProps(t).LRUConf

	err := api.SetBucketProps(tutils.DefaultBaseAPIParams(t), TestLocalBucketName, bucketProps)
	tutils.CheckFatal(err, t)

	p, err := api.HeadBucket(tutils.DefaultBaseAPIParams(t), TestLocalBucketName)
	tutils.CheckFatal(err, t)

	// check that bucket props do get set
	validateBucketProps(t, bucketProps, *p)
	err = api.ResetBucketProps(tutils.DefaultBaseAPIParams(t), TestLocalBucketName)
	tutils.CheckFatal(err, t)

	p, err = api.HeadBucket(tutils.DefaultBaseAPIParams(t), TestLocalBucketName)
	tutils.CheckFatal(err, t)

	// check that bucket props are reset
	validateBucketProps(t, globalProps, *p)
}

func TestSetBucketNextTierURLInvalid(t *testing.T) {
	var (
		proxyURL          = getPrimaryURL(t, proxyURLReadOnly)
		bucketProps       cmn.BucketProps
		invalidDaemonURLs []string
	)

	tutils.CreateFreshLocalBucket(t, proxyURL, TestLocalBucketName)
	defer tutils.DestroyLocalBucket(t, proxyURL, TestLocalBucketName)

	smap := getClusterMap(t, proxyURL)

	if len(smap.Tmap) < 1 || len(smap.Pmap) < 1 {
		t.Fatal("This test requires there to be at least one target and one proxy in the current cluster")
	}

	// Test Invalid Proxy URLs for NextTierURL property
	for _, proxyInfo := range smap.Pmap {
		invalidDaemonURLs = append(invalidDaemonURLs,
			proxyInfo.PublicNet.DirectURL,
			proxyInfo.IntraControlNet.DirectURL,
			proxyInfo.IntraDataNet.DirectURL,
		)
		// Break early to avoid flooding the logs with too many error messages.
		break
	}

	// Test Invalid Target URLs for NextTierURL property
	for _, targetInfo := range smap.Tmap {
		invalidDaemonURLs = append(invalidDaemonURLs,
			targetInfo.PublicNet.DirectURL,
			targetInfo.IntraControlNet.DirectURL,
			targetInfo.IntraDataNet.DirectURL,
		)
		// Break early to avoid flooding the logs with too many error messages.
		break
	}

	for _, url := range invalidDaemonURLs {
		bucketProps.NextTierURL = url
		if err := api.SetBucketProps(tutils.DefaultBaseAPIParams(t), TestLocalBucketName, bucketProps); err == nil {
			t.Fatalf("Setting the bucket's nextTierURL to daemon %q should fail, it is in the current cluster.", url)
		}
	}
}

func TestListObjects(t *testing.T) {
	var (
		iterations  = 20
		workerCount = 10
		dirLen      = 10
		objectSize  = cmn.KiB

		bucket   = t.Name() + "Bucket"
		proxyURL = getPrimaryURL(t, proxyURLReadOnly)
		wg       = &sync.WaitGroup{}
		random   = rand.New(rand.NewSource(time.Now().UnixNano()))
	)

	tutils.CreateFreshLocalBucket(t, proxyURL, bucket)
	defer tutils.DestroyLocalBucket(t, proxyURL, bucket)

	// Iterations of PUT
	totalObjects := 0
	for iter := 0; iter < iterations; iter++ {
		objectCount := random.Intn(1024) + 1000
		totalObjects += objectCount
		for wid := 0; wid < workerCount; wid++ {
			wg.Add(1)
			go func(wid int) {
				reader, err := tutils.NewRandReader(int64(objectSize), true)
				tutils.CheckFatal(err, t)
				objDir := tutils.RandomObjDir(random, dirLen, 5)
				objectsToPut := objectCount / workerCount
				if wid == workerCount-1 { // last worker puts leftovers
					objectsToPut += objectCount % workerCount
				}
				putRR(t, wid, proxyURL, reader, bucket, objDir, objectsToPut)
				wg.Done()
			}(wid)
		}
		wg.Wait()

		// Confirm PUTs
		bckObjs, err := tutils.ListObjects(proxyURL, bucket, "", 0)
		tutils.CheckFatal(err, t)

		if len(bckObjs) != totalObjects {
			t.Errorf("actual objects %d, expected: %d", len(bckObjs), totalObjects)
		}
	}
}
