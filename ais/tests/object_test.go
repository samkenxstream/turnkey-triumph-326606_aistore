// Package integration contains AIS integration tests.
/*
 * Copyright (c) 2018-2020, NVIDIA CORPORATION. All rights reserved.
 */
package integration

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/aistore/api"
	"github.com/NVIDIA/aistore/cluster"
	"github.com/NVIDIA/aistore/cluster/mock"
	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/cmn/cos"
	"github.com/NVIDIA/aistore/containers"
	"github.com/NVIDIA/aistore/devtools/readers"
	"github.com/NVIDIA/aistore/devtools/tassert"
	"github.com/NVIDIA/aistore/devtools/tlog"
	"github.com/NVIDIA/aistore/devtools/tutils"
)

const (
	httpBucketURL           = "http://storage.googleapis.com/minikube/"
	httpObjectName          = "minikube-0.6.iso.sha256"
	httpObjectURL           = httpBucketURL + httpObjectName
	httpAnotherObjectName   = "minikube-0.7.iso.sha256"
	httpAnotherObjectURL    = httpBucketURL + httpAnotherObjectName
	httpObjectOutput        = "ff0f444f4a01f0ec7925e6bb0cb05e84156cff9cc8de6d03102d8b3df35693e2"
	httpAnotherObjectOutput = "aadc8b6f5720d5a493a36e1f07f71bffb588780c76498d68cd761793d2ca344e"
)

const (
	getOP = "GET"
	putOP = "PUT"
)

func TestObjectInvalidName(t *testing.T) {
	var (
		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams()
		bck        = cmn.Bck{
			Name:     cos.RandString(10),
			Provider: cmn.ProviderAIS,
		}
	)

	tests := []struct {
		op      string
		objName string
	}{
		{op: putOP, objName: "."},
		{op: putOP, objName: ".."},
		{op: putOP, objName: "../smth.txt"},
		{op: putOP, objName: "../."},
		{op: putOP, objName: "../.."},
		{op: putOP, objName: "///"},
		{op: putOP, objName: ""},
		{op: putOP, objName: "1\x00"},
		{op: putOP, objName: "\n"},

		{op: getOP, objName: ""},
		{op: getOP, objName: ".."},
		{op: getOP, objName: "///"},
		{op: getOP, objName: "...../log/aisnode.INFO"},
		{op: getOP, objName: "/...../log/aisnode.INFO"},
		{op: getOP, objName: "/\\../\\../\\../\\../log/aisnode.INFO"},
		{op: getOP, objName: "\\../\\../\\../\\../log/aisnode.INFO"},
		{op: getOP, objName: "/../../../../log/aisnode.INFO"},
		{op: getOP, objName: "/././../../../../log/aisnode.INFO"},
	}

	tutils.CreateFreshBucket(t, proxyURL, bck, nil)

	for _, test := range tests {
		t.Run(test.op, func(t *testing.T) {
			switch test.op {
			case putOP:
				reader, err := readers.NewRandReader(cos.KiB, cos.ChecksumNone)
				tassert.CheckFatal(t, err)
				err = api.PutObject(api.PutObjectArgs{
					BaseParams: baseParams,
					Bck:        bck,
					Object:     test.objName,
					Reader:     reader,
				})
				tassert.Errorf(t, err != nil, "expected error to occur (object name: %q)", test.objName)
			case getOP:
				_, err := api.GetObjectWithValidation(baseParams, bck, test.objName)
				tassert.Errorf(t, err != nil, "expected error to occur (object name: %q)", test.objName)
			default:
				panic(test.op)
			}
		})
	}
}

func TestRemoteBucketObject(t *testing.T) {
	var (
		baseParams = tutils.BaseAPIParams()
		bck        = cliBck
	)

	tutils.CheckSkip(t, tutils.SkipTestArgs{Long: true, RemoteBck: true, Bck: bck})

	tests := []struct {
		ty     string
		exists bool
	}{
		{putOP, false},
		{putOP, true},
		{getOP, false},
		{getOP, true},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%s:%v", test.ty, test.exists), func(t *testing.T) {
			object := cos.RandString(10)
			if !test.exists {
				bck.Name = cos.RandString(10)
			} else {
				bck.Name = cliBck.Name
			}

			reader, err := readers.NewRandReader(cos.KiB, cos.ChecksumNone)
			tassert.CheckFatal(t, err)

			defer api.DeleteObject(baseParams, bck, object)

			switch test.ty {
			case putOP:
				err = api.PutObject(api.PutObjectArgs{
					BaseParams: baseParams,
					Bck:        bck,
					Object:     object,
					Reader:     reader,
				})
			case getOP:
				if test.exists {
					err = api.PutObject(api.PutObjectArgs{
						BaseParams: baseParams,
						Bck:        bck,
						Object:     object,
						Reader:     reader,
					})
					tassert.CheckFatal(t, err)
				}

				_, err = api.GetObjectWithValidation(baseParams, bck, object)
			default:
				t.Fail()
			}

			if !test.exists {
				if err == nil {
					t.Errorf("expected error when doing %s on non existing %q bucket", test.ty, bck)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error when executing %s on existing %q bucket(err = %v)", test.ty, bck, err)
				}
			}
		})
	}
}

func TestHttpProviderObjectGet(t *testing.T) {
	var (
		proxyURL   = tutils.RandomProxyURL()
		baseParams = tutils.BaseAPIParams(proxyURL)
		hbo, _     = cmn.NewHTTPObjPath(httpObjectURL)
		w          = bytes.NewBuffer(nil)
		options    = api.GetObjectInput{Writer: w}
	)
	_ = api.DestroyBucket(baseParams, hbo.Bck)
	defer api.DestroyBucket(baseParams, hbo.Bck)

	// get using the HTTP API
	options.Query = make(url.Values, 1)
	options.Query.Set(cmn.URLParamOrigURL, httpObjectURL)
	_, err := api.GetObject(baseParams, hbo.Bck, httpObjectName, options)
	tassert.CheckFatal(t, err)
	tassert.Fatalf(t, strings.TrimSpace(w.String()) == httpObjectOutput, "bad content (expected:%s got:%s)", httpObjectOutput, w.String())

	// get another object using /v1/objects/bucket-name/object-name endpoint
	w.Reset()
	options.Query = make(url.Values, 1)
	options.Query.Set(cmn.URLParamOrigURL, httpAnotherObjectURL)
	_, err = api.GetObject(baseParams, hbo.Bck, httpAnotherObjectName, options)
	tassert.CheckFatal(t, err)
	tassert.Fatalf(t, strings.TrimSpace(w.String()) == httpAnotherObjectOutput, "bad content (expected:%s got:%s)", httpAnotherObjectOutput, w.String())

	// list object should contain both the objects
	reslist, err := api.ListObjects(baseParams, hbo.Bck, &cmn.SelectMsg{}, 0)
	tassert.CheckFatal(t, err)
	tassert.Errorf(t, len(reslist.Entries) == 2, "should have exactly 2 entries in bucket")

	matchCount := 0
	for _, entry := range reslist.Entries {
		if entry.Name == httpAnotherObjectName || entry.Name == httpObjectName {
			matchCount++
		}
	}
	tassert.Errorf(t, matchCount == 2, "objects %s and %s should be present in the bucket %s", httpObjectName, httpAnotherObjectName, hbo.Bck)
}

func TestAppendObject(t *testing.T) {
	for _, cksumType := range cos.SupportedChecksums() {
		t.Run(cksumType, func(t *testing.T) {
			var (
				proxyURL   = tutils.RandomProxyURL(t)
				baseParams = tutils.BaseAPIParams(proxyURL)
				bck        = cmn.Bck{
					Name:     testBucketName,
					Provider: cmn.ProviderAIS,
				}
				objName = "test/obj1"

				objHead = "1111111111"
				objBody = "222222222222222"
				objTail = "333333333"
				content = objHead + objBody + objTail
				objSize = len(content)
			)
			tutils.CreateFreshBucket(t, proxyURL, bck, &cmn.BucketPropsToUpdate{
				Cksum: &cmn.CksumConfToUpdate{
					Type: api.String(cksumType),
				},
			})

			var (
				err    error
				handle string
				cksum  = cos.NewCksumHash(cksumType)
			)
			for _, body := range []string{objHead, objBody, objTail} {
				args := api.AppendArgs{
					BaseParams: baseParams,
					Bck:        bck,
					Object:     objName,
					Handle:     handle,
					Reader:     cos.NewByteHandle([]byte(body)),
				}
				handle, err = api.AppendObject(args)
				tassert.CheckFatal(t, err)

				_, err = cksum.H.Write([]byte(body))
				tassert.CheckFatal(t, err)
			}

			// Flush object with cksum to make it persistent in the bucket.
			cksum.Finalize()
			err = api.FlushObject(api.FlushArgs{
				BaseParams: baseParams,
				Bck:        bck,
				Object:     objName,
				Handle:     handle,
				Cksum:      cksum.Clone(),
			})
			tassert.CheckFatal(t, err)

			// Read the object from the bucket.
			writer := bytes.NewBuffer(nil)
			getArgs := api.GetObjectInput{Writer: writer}
			n, err := api.GetObjectWithValidation(baseParams, bck, objName, getArgs)
			tassert.CheckFatal(t, err)
			tassert.Errorf(
				t, writer.String() == content,
				"invalid object content: [%d](%s), expected: [%d](%s)",
				n, writer.String(), objSize, content,
			)
		})
	}
}

func Test_SameLocalAndRemoteBckNameValidate(t *testing.T) {
	var (
		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
		bckLocal   = cmn.Bck{
			Name:     cliBck.Name,
			Provider: cmn.ProviderAIS,
		}
		bckRemote  = cliBck
		fileName1  = "mytestobj1.txt"
		fileName2  = "mytestobj2.txt"
		objRange   = "mytestobj{1..2}.txt"
		dataLocal  = []byte("im local")
		dataRemote = []byte("I'm from the cloud!")
		files      = []string{fileName1, fileName2}
	)

	tutils.CheckSkip(t, tutils.SkipTestArgs{RemoteBck: true, Bck: bckRemote})

	putArgsLocal := api.PutObjectArgs{
		BaseParams: baseParams,
		Bck:        bckLocal,
		Object:     fileName1,
		Reader:     readers.NewBytesReader(dataLocal),
	}

	putArgsRemote := api.PutObjectArgs{
		BaseParams: baseParams,
		Bck:        bckRemote,
		Object:     fileName1,
		Reader:     readers.NewBytesReader(dataRemote),
	}

	// PUT/GET/DEL Without ais bucket
	tlog.Logf("Validating responses for non-existent ais bucket...\n")
	err := api.PutObject(putArgsLocal)
	if err == nil {
		t.Fatalf("ais bucket %s does not exist: Expected an error.", bckLocal)
	}

	_, err = api.GetObject(baseParams, bckLocal, fileName1)
	if err == nil {
		t.Fatalf("ais bucket %s does not exist: Expected an error.", bckLocal)
	}

	err = api.DeleteObject(baseParams, bckLocal, fileName1)
	if err == nil {
		t.Fatalf("ais bucket %s does not exist: Expected an error.", bckLocal)
	}

	tlog.Logf("PrefetchList %d\n", len(files))
	prefetchListID, err := api.PrefetchList(baseParams, bckRemote, files)
	tassert.CheckFatal(t, err)
	args := api.XactReqArgs{ID: prefetchListID, Kind: cmn.ActPrefetchObjects, Timeout: rebalanceTimeout}
	_, err = api.WaitForXaction(baseParams, args)
	tassert.CheckFatal(t, err)

	tlog.Logf("PrefetchRange\n")
	prefetchRangeID, err := api.PrefetchRange(baseParams, bckRemote, objRange)
	tassert.CheckFatal(t, err)
	args = api.XactReqArgs{ID: prefetchRangeID, Kind: cmn.ActPrefetchObjects, Timeout: rebalanceTimeout}
	_, err = api.WaitForXaction(baseParams, args)
	tassert.CheckFatal(t, err)

	tlog.Logf("EvictList\n")
	evictListID, err := api.EvictList(baseParams, bckRemote, files)
	tassert.CheckFatal(t, err)
	args = api.XactReqArgs{ID: evictListID, Kind: cmn.ActEvictObjects, Timeout: rebalanceTimeout}
	_, err = api.WaitForXaction(baseParams, args)
	tassert.CheckFatal(t, err)

	tlog.Logf("EvictRange\n")
	evictRangeID, err := api.EvictRange(baseParams, bckRemote, objRange)
	tassert.CheckFatal(t, err)
	args = api.XactReqArgs{ID: evictRangeID, Kind: cmn.ActEvictObjects, Timeout: rebalanceTimeout}
	_, err = api.WaitForXaction(baseParams, args)
	tassert.CheckFatal(t, err)

	tutils.CreateFreshBucket(t, proxyURL, bckLocal, nil)

	// PUT
	tlog.Logf("Putting %s and %s into buckets...\n", fileName1, fileName2)
	err = api.PutObject(putArgsLocal)
	tassert.CheckFatal(t, err)
	putArgsLocal.Object = fileName2
	err = api.PutObject(putArgsLocal)
	tassert.CheckFatal(t, err)

	err = api.PutObject(putArgsRemote)
	tassert.CheckFatal(t, err)
	putArgsRemote.Object = fileName2
	err = api.PutObject(putArgsRemote)
	tassert.CheckFatal(t, err)

	// Check ais bucket has 2 objects
	tlog.Logf("Validating ais bucket have %s and %s ...\n", fileName1, fileName2)
	_, err = api.HeadObject(baseParams, bckLocal, fileName1)
	tassert.CheckFatal(t, err)
	_, err = api.HeadObject(baseParams, bckLocal, fileName2)
	tassert.CheckFatal(t, err)

	// Prefetch/Evict should work
	prefetchListID, err = api.PrefetchList(baseParams, bckRemote, files)
	tassert.CheckFatal(t, err)
	args = api.XactReqArgs{ID: prefetchListID, Kind: cmn.ActPrefetchObjects, Timeout: rebalanceTimeout}
	_, err = api.WaitForXaction(baseParams, args)
	tassert.CheckFatal(t, err)

	evictListID, err = api.EvictList(baseParams, bckRemote, files)
	tassert.CheckFatal(t, err)
	args = api.XactReqArgs{ID: evictListID, Kind: cmn.ActEvictObjects, Timeout: rebalanceTimeout}
	_, err = api.WaitForXaction(baseParams, args)
	tassert.CheckFatal(t, err)

	// Deleting from cloud bucket
	tlog.Logf("Deleting %s and %s from cloud bucket ...\n", fileName1, fileName2)
	deleteID, err := api.DeleteList(baseParams, bckRemote, files)
	tassert.CheckFatal(t, err)
	args = api.XactReqArgs{ID: deleteID, Kind: cmn.ActDeleteObjects, Timeout: rebalanceTimeout}
	_, err = api.WaitForXaction(baseParams, args)
	tassert.CheckFatal(t, err)

	// Deleting from ais bucket
	tlog.Logf("Deleting %s and %s from ais bucket ...\n", fileName1, fileName2)
	deleteID, err = api.DeleteList(baseParams, bckLocal, files)
	tassert.CheckFatal(t, err)
	args = api.XactReqArgs{ID: deleteID, Kind: cmn.ActDeleteObjects, Timeout: rebalanceTimeout}
	_, err = api.WaitForXaction(baseParams, args)
	tassert.CheckFatal(t, err)

	_, err = api.HeadObject(baseParams, bckLocal, fileName1)
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(http.StatusNotFound)) {
		t.Errorf("Local file %s not deleted", fileName1)
	}
	_, err = api.HeadObject(baseParams, bckLocal, fileName2)
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(http.StatusNotFound)) {
		t.Errorf("Local file %s not deleted", fileName2)
	}

	_, err = api.HeadObject(baseParams, bckRemote, fileName1)
	if err == nil {
		t.Errorf("remote file %s not deleted", fileName1)
	}
	_, err = api.HeadObject(baseParams, bckRemote, fileName2)
	if err == nil {
		t.Errorf("remote file %s not deleted", fileName2)
	}
}

func Test_SameAISAndRemoteBucketName(t *testing.T) {
	var (
		defLocalProps  cmn.BucketPropsToUpdate
		defRemoteProps cmn.BucketPropsToUpdate

		bckLocal = cmn.Bck{
			Name:     cliBck.Name,
			Provider: cmn.ProviderAIS,
		}
		bckRemote  = cliBck
		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
		fileName   = "mytestobj1.txt"
		dataLocal  = []byte("im local")
		dataRemote = []byte("I'm from the cloud!")
		msg        = &cmn.SelectMsg{Props: "size,status", Prefix: "my"}
		found      = false
	)

	tutils.CheckSkip(t, tutils.SkipTestArgs{RemoteBck: true, Bck: bckRemote})

	tutils.CreateFreshBucket(t, proxyURL, bckLocal, nil)

	bucketPropsLocal := &cmn.BucketPropsToUpdate{
		Cksum: &cmn.CksumConfToUpdate{
			Type: api.String(cos.ChecksumNone),
		},
	}
	bucketPropsRemote := &cmn.BucketPropsToUpdate{}

	// Put
	tlog.Logf("Putting object (%s) into ais bucket %s...\n", fileName, bckLocal)
	putArgs := api.PutObjectArgs{
		BaseParams: baseParams,
		Bck:        bckLocal,
		Object:     fileName,
		Reader:     readers.NewBytesReader(dataLocal),
	}
	err := api.PutObject(putArgs)
	tassert.CheckFatal(t, err)

	resLocal, err := api.ListObjects(baseParams, bckLocal, msg, 0)
	tassert.CheckFatal(t, err)

	tlog.Logf("Putting object (%s) into cloud bucket %s...\n", fileName, bckRemote)
	putArgs = api.PutObjectArgs{
		BaseParams: baseParams,
		Bck:        bckRemote,
		Object:     fileName,
		Reader:     readers.NewBytesReader(dataRemote),
	}
	err = api.PutObject(putArgs)
	tassert.CheckFatal(t, err)

	resRemote, err := api.ListObjects(baseParams, bckRemote, msg, 0)
	tassert.CheckFatal(t, err)

	if len(resLocal.Entries) != 1 {
		t.Fatalf("Expected number of files in ais bucket (%s) does not match: expected %v, got %v",
			bckRemote, 1, len(resLocal.Entries))
	}

	for _, entry := range resRemote.Entries {
		if entry.Name == fileName {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("File (%s) not found in cloud bucket (%s)", fileName, bckRemote)
	}

	// Get
	lenLocal, err := api.GetObject(baseParams, bckLocal, fileName)
	tassert.CheckFatal(t, err)
	lenRemote, err := api.GetObject(baseParams, bckRemote, fileName)
	tassert.CheckFatal(t, err)

	if lenLocal == lenRemote {
		t.Errorf("Local file and cloud file have same size, expected: local (%v) cloud (%v) got: local (%v) cloud (%v)",
			len(dataLocal), len(dataRemote), lenLocal, lenRemote)
	}

	// Delete
	err = api.DeleteObject(baseParams, bckRemote, fileName)
	tassert.CheckFatal(t, err)

	lenLocal, err = api.GetObject(baseParams, bckLocal, fileName)
	tassert.CheckFatal(t, err)

	// Check that local object still exists
	if lenLocal != int64(len(dataLocal)) {
		t.Errorf("Local file %s deleted", fileName)
	}

	// Check that cloud object is deleted using HeadObject
	_, err = api.HeadObject(baseParams, bckRemote, fileName)
	if !strings.Contains(err.Error(), strconv.Itoa(http.StatusNotFound)) {
		t.Errorf("Remote file %s not deleted", fileName)
	}

	// Set Props Object
	_, err = api.SetBucketProps(baseParams, bckLocal, bucketPropsLocal)
	tassert.CheckFatal(t, err)

	_, err = api.SetBucketProps(baseParams, bckRemote, bucketPropsRemote)
	tassert.CheckFatal(t, err)

	// Validate ais bucket props are set
	localProps, err := api.HeadBucket(baseParams, bckLocal)
	tassert.CheckFatal(t, err)
	validateBucketProps(t, bucketPropsLocal, localProps)

	// Validate cloud bucket props are set
	cloudProps, err := api.HeadBucket(baseParams, bckRemote)
	tassert.CheckFatal(t, err)
	validateBucketProps(t, bucketPropsRemote, cloudProps)

	// Reset ais bucket props and validate they are reset
	_, err = api.ResetBucketProps(baseParams, bckLocal)
	tassert.CheckFatal(t, err)
	localProps, err = api.HeadBucket(baseParams, bckLocal)
	tassert.CheckFatal(t, err)
	validateBucketProps(t, &defLocalProps, localProps)

	// Check if cloud bucket props remain the same
	cloudProps, err = api.HeadBucket(baseParams, bckRemote)
	tassert.CheckFatal(t, err)
	validateBucketProps(t, bucketPropsRemote, cloudProps)

	// Reset cloud bucket props
	_, err = api.ResetBucketProps(baseParams, bckRemote)
	tassert.CheckFatal(t, err)
	cloudProps, err = api.HeadBucket(baseParams, bckRemote)
	tassert.CheckFatal(t, err)
	validateBucketProps(t, &defRemoteProps, cloudProps)

	// Check if ais bucket props remain the same
	localProps, err = api.HeadBucket(baseParams, bckLocal)
	tassert.CheckFatal(t, err)
	validateBucketProps(t, &defLocalProps, localProps)
}

func Test_coldgetmd5(t *testing.T) {
	var (
		m = ioContext{
			t:        t,
			bck:      cliBck,
			num:      5,
			fileSize: largeFileSize,
			prefix:   "md5/obj-",
		}
		totalSize     = int64(uint64(m.num) * m.fileSize)
		proxyURL      = tutils.RandomProxyURL(t)
		propsToUpdate *cmn.BucketPropsToUpdate
	)

	tutils.CheckSkip(t, tutils.SkipTestArgs{RemoteBck: true, Bck: m.bck})

	m.initAndSaveCluState()

	baseParams := tutils.BaseAPIParams(proxyURL)
	p, err := api.HeadBucket(baseParams, m.bck)
	tassert.CheckFatal(t, err)

	t.Cleanup(func() {
		propsToUpdate = &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				ValidateColdGet: api.Bool(p.Cksum.ValidateColdGet),
			},
		}
		_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckError(t, err)
		m.del()
	})

	m.remotePuts(true /*evict*/)

	// Disable Cold Get Validation
	if p.Cksum.ValidateColdGet {
		propsToUpdate = &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				ValidateColdGet: api.Bool(false),
			},
		}
		_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckFatal(t, err)
	}

	start := time.Now()
	m.gets(false /*withValidation*/)
	tlog.Logf("GET %s without MD5 validation: %v\n", cos.B2S(totalSize, 0), time.Since(start))

	m.evict()

	// Enable cold get validation.
	propsToUpdate = &cmn.BucketPropsToUpdate{
		Cksum: &cmn.CksumConfToUpdate{
			ValidateColdGet: api.Bool(true),
		},
	}
	_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
	tassert.CheckFatal(t, err)

	start = time.Now()
	m.gets(true /*withValidation*/)
	tlog.Logf("GET %s with MD5 validation:    %v\n", cos.B2S(totalSize, 0), time.Since(start))
}

func TestHeadBucket(t *testing.T) {
	var (
		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
		bck        = cmn.Bck{
			Name:     testBucketName,
			Provider: cmn.ProviderAIS,
		}
	)

	tutils.CreateFreshBucket(t, proxyURL, bck, nil)

	bckPropsToUpdate := &cmn.BucketPropsToUpdate{
		Cksum: &cmn.CksumConfToUpdate{
			ValidateWarmGet: api.Bool(true),
		},
		LRU: &cmn.LRUConfToUpdate{
			Enabled: api.Bool(true),
		},
	}
	_, err := api.SetBucketProps(baseParams, bck, bckPropsToUpdate)
	tassert.CheckFatal(t, err)

	p, err := api.HeadBucket(baseParams, bck)
	tassert.CheckFatal(t, err)

	validateBucketProps(t, bckPropsToUpdate, p)
}

func TestHeadRemoteBucket(t *testing.T) {
	var (
		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
		bck        = cliBck
	)

	tutils.CheckSkip(t, tutils.SkipTestArgs{RemoteBck: true, Bck: bck})

	bckPropsToUpdate := &cmn.BucketPropsToUpdate{
		Cksum: &cmn.CksumConfToUpdate{
			ValidateWarmGet: api.Bool(true),
			ValidateColdGet: api.Bool(true),
		},
		LRU: &cmn.LRUConfToUpdate{
			Enabled: api.Bool(true),
		},
	}
	_, err := api.SetBucketProps(baseParams, bck, bckPropsToUpdate)
	tassert.CheckFatal(t, err)
	defer resetBucketProps(t, proxyURL, bck)

	p, err := api.HeadBucket(baseParams, bck)
	tassert.CheckFatal(t, err)
	validateBucketProps(t, bckPropsToUpdate, p)
}

func TestHeadNonexistentBucket(t *testing.T) {
	var (
		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
	)

	bucket, err := tutils.GenerateNonexistentBucketName("head", baseParams)
	tassert.CheckFatal(t, err)

	bck := cmn.Bck{
		Name:     bucket,
		Provider: cmn.ProviderAIS,
	}

	_, err = api.HeadBucket(baseParams, bck)
	if err == nil {
		t.Fatalf("Expected an error, but go no errors.")
	}

	status := api.HTTPStatus(err)
	if status != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, status)
	}
}

// 1.	PUT file
// 2.	Change contents of the file or change XXHash
// 3.	GET file.
// NOTE: The following test can only work when running on a local setup
// (targets are co-located with where this test is running from, because
// it searches a local oldFileIfo system).
func TestChecksumValidateOnWarmGetForRemoteBucket(t *testing.T) {
	var (
		m = ioContext{
			t:        t,
			bck:      cliBck,
			num:      3,
			fileSize: cos.KiB,
			prefix:   "cksum/obj-",
		}

		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
	)

	m.init()

	tutils.CheckSkip(t, tutils.SkipTestArgs{RemoteBck: true, Bck: m.bck})

	if containers.DockerRunning() {
		t.Skip(fmt.Sprintf("test %q requires Xattributes to be set, doesn't work with docker", t.Name()))
	}

	p, err := api.HeadBucket(baseParams, m.bck)
	tassert.CheckFatal(t, err)

	_ = mock.NewTarget(cluster.NewBaseBownerMock(
		cluster.NewBck(
			m.bck.Name, m.bck.Provider, cmn.NsGlobal,
			&cmn.BucketProps{Cksum: cmn.CksumConf{Type: cos.ChecksumXXHash}, Extra: p.Extra, BID: 0xa73b9f11},
		),
	), pmm, smm)

	initMountpaths(t, proxyURL)

	m.puts()

	t.Cleanup(func() {
		propsToUpdate := &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				Type:            api.String(p.Cksum.Type),
				ValidateWarmGet: api.Bool(p.Cksum.ValidateWarmGet),
			},
		}
		_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckError(t, err)
		m.del()
	})

	objName := m.objNames[0]
	_, err = api.GetObjectWithValidation(baseParams, m.bck, objName)
	tassert.CheckError(t, err)

	if !p.Cksum.ValidateWarmGet {
		propsToUpdate := &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				ValidateWarmGet: api.Bool(true),
			},
		}
		_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckFatal(t, err)
	}

	fqn := findObjOnDisk(m.bck, objName)
	tutils.CheckPathExists(t, fqn, false /*dir*/)
	oldFileInfo, _ := os.Stat(fqn)

	// Test when the contents of the file are changed
	tlog.Logf("Changing contents of the file [%s]: %s\n", objName, fqn)
	err = os.WriteFile(fqn, []byte("Contents of this file have been changed."), cos.PermRWR)
	tassert.CheckFatal(t, err)
	validateGETUponFileChangeForChecksumValidation(t, proxyURL, objName, fqn, oldFileInfo)

	// Test when the xxHash of the file is changed
	objName = m.objNames[1]
	fqn = findObjOnDisk(m.bck, objName)
	tutils.CheckPathExists(t, fqn, false /*dir*/)
	oldFileInfo, _ = os.Stat(fqn)

	tlog.Logf("Changing file xattr[%s]: %s\n", objName, fqn)
	err = tutils.SetXattrCksum(fqn, m.bck, cos.NewCksum(cos.ChecksumXXHash, "01234"))
	tassert.CheckError(t, err)
	validateGETUponFileChangeForChecksumValidation(t, proxyURL, objName, fqn, oldFileInfo)

	// Test for no checksum algo
	objName = m.objNames[2]
	fqn = findObjOnDisk(m.bck, objName)

	if p.Cksum.Type != cos.ChecksumNone {
		propsToUpdate := &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				Type: api.String(cos.ChecksumNone),
			},
		}
		_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckFatal(t, err)
	}

	tlog.Logf("Changing file xattr[%s]: %s\n", objName, fqn)
	err = tutils.SetXattrCksum(fqn, m.bck, cos.NewCksum(cos.ChecksumXXHash, "01234abcde"))
	tassert.CheckError(t, err)

	_, err = api.GetObject(baseParams, m.bck, objName)
	tassert.Errorf(t, err == nil, "A GET on an object when checksum algo is none should pass. Error: %v", err)
}

func TestEvictRemoteBucket(t *testing.T) {
	t.Run("Cloud/KeepMD", func(t *testing.T) { testEvictRemoteBucket(t, cliBck, true) })
	t.Run("Cloud/DeleteMD", func(t *testing.T) { testEvictRemoteBucket(t, cliBck, false) })
	t.Run("RemoteAIS", testEvictRemoteAISBucket)
}

func testEvictRemoteAISBucket(t *testing.T) {
	tutils.CheckSkip(t, tutils.SkipTestArgs{RequiresRemoteCluster: true})
	bck := cmn.Bck{
		Name:     cos.RandString(10),
		Provider: cmn.ProviderAIS,
		Ns: cmn.Ns{
			UUID: tutils.RemoteCluster.UUID,
		},
	}
	tutils.CreateFreshBucket(t, proxyURL, bck, nil)
	testEvictRemoteBucket(t, bck, false)
}

func testEvictRemoteBucket(t *testing.T, bck cmn.Bck, keepMD bool) {
	var (
		m = ioContext{
			t:        t,
			bck:      bck,
			num:      5,
			fileSize: largeFileSize,
			prefix:   "evict/obj-",
		}
		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
	)

	tutils.CheckSkip(t, tutils.SkipTestArgs{RemoteBck: true, Bck: m.bck})
	m.initAndSaveCluState()

	t.Cleanup(func() {
		m.del()
		resetBucketProps(t, proxyURL, m.bck)
	})

	m.remotePuts(false /*evict*/)

	// Test property, mirror is disabled for cloud bucket that hasn't been accessed,
	// even if system config says otherwise
	_, err := api.SetBucketProps(baseParams, m.bck, &cmn.BucketPropsToUpdate{
		Mirror: &cmn.MirrorConfToUpdate{Enabled: api.Bool(true)},
	})
	tassert.CheckFatal(t, err)

	bProps, err := api.HeadBucket(baseParams, m.bck)
	tassert.CheckFatal(t, err)
	tassert.Fatalf(t, bProps.Mirror.Enabled, "test property hasn't changed")

	err = api.EvictRemoteBucket(baseParams, m.bck, keepMD)
	tassert.CheckFatal(t, err)

	for _, objName := range m.objNames {
		exists := tutils.CheckObjExists(proxyURL, m.bck, objName)
		tassert.Errorf(t, !exists, "object remains cached: %s", objName)
	}
	bProps, err = api.HeadBucket(baseParams, m.bck)
	tassert.CheckFatal(t, err)
	if keepMD {
		tassert.Fatalf(t, bProps.Mirror.Enabled, "test property was reset")
	} else {
		tassert.Fatalf(t, !bProps.Mirror.Enabled, "test property not reset")
	}
}

func validateGETUponFileChangeForChecksumValidation(t *testing.T, proxyURL, objName, fqn string,
	oldFileInfo os.FileInfo) {
	// Do a GET to see to check if a cold get was executed by comparing old and new size
	var (
		baseParams = tutils.BaseAPIParams(proxyURL)
		bck        = cliBck
	)

	_, err := api.GetObjectWithValidation(baseParams, bck, objName)
	tassert.CheckError(t, err)

	tutils.CheckPathExists(t, fqn, false /*dir*/)
	newFileInfo, _ := os.Stat(fqn)
	if newFileInfo.Size() != oldFileInfo.Size() {
		t.Errorf("Expected size: %d, Actual Size: %d", oldFileInfo.Size(), newFileInfo.Size())
	}
}

// 1.	PUT file
// 2.	Change contents of the file or change XXHash
// 3.	GET file (first GET should fail with Internal Server Error and the
// 		second should fail with not found).
// Note: The following test can only work when running on a local setup
// (targets are co-located with where this test is running from, because
// it searches a local file system)
func TestChecksumValidateOnWarmGetForBucket(t *testing.T) {
	var (
		m = ioContext{
			t: t,
			bck: cmn.Bck{
				Provider: cmn.ProviderAIS,
				Name:     cos.RandString(15),
				Props:    &cmn.BucketProps{BID: 2},
			},
			num:      3,
			fileSize: cos.KiB,
		}

		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
		_          = mock.NewTarget(cluster.NewBaseBownerMock(
			cluster.NewBck(
				m.bck.Name, cmn.ProviderAIS, cmn.NsGlobal,
				&cmn.BucketProps{Cksum: cmn.CksumConf{Type: cos.ChecksumXXHash}, BID: 1},
			),
			cluster.NewBckEmbed(m.bck),
		), pmm, smm)
		cksumConf = cmn.DefaultBckProps(m.bck).Cksum
	)

	m.init()

	if containers.DockerRunning() {
		t.Skip(fmt.Sprintf("test %q requires write access to xattrs, doesn't work with docker", t.Name()))
	}

	initMountpaths(t, proxyURL)

	tutils.CreateFreshBucket(t, proxyURL, m.bck, nil)

	m.puts()

	if !cksumConf.ValidateWarmGet {
		propsToUpdate := &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				ValidateWarmGet: api.Bool(true),
			},
		}
		_, err := api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckFatal(t, err)
	}

	// Test changing the file content.
	objName := m.objNames[0]
	fqn := findObjOnDisk(m.bck, objName)
	tlog.Logf("Changing contents of the file [%s]: %s\n", objName, fqn)
	err := os.WriteFile(fqn, []byte("Contents of this file have been changed."), cos.PermRWR)
	tassert.CheckFatal(t, err)
	executeTwoGETsForChecksumValidation(proxyURL, m.bck, objName, t)

	// Test changing the file xattr.
	objName = m.objNames[1]
	fqn = findObjOnDisk(m.bck, objName)
	tlog.Logf("Changing file xattr[%s]: %s\n", objName, fqn)
	err = tutils.SetXattrCksum(fqn, m.bck, cos.NewCksum(cos.ChecksumXXHash, "01234abcde"))
	tassert.CheckError(t, err)
	executeTwoGETsForChecksumValidation(proxyURL, m.bck, objName, t)

	// Test for none checksum algorithm.
	objName = m.objNames[2]
	fqn = findObjOnDisk(m.bck, objName)

	if cksumConf.Type != cos.ChecksumNone {
		propsToUpdate := &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				Type: api.String(cos.ChecksumNone),
			},
		}
		_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckFatal(t, err)
	}

	tlog.Logf("Changing file xattr[%s]: %s\n", objName, fqn)
	err = tutils.SetXattrCksum(fqn, m.bck, cos.NewCksum(cos.ChecksumXXHash, "01234abcde"))
	tassert.CheckError(t, err)
	_, err = api.GetObject(baseParams, m.bck, objName)
	tassert.CheckError(t, err)
}

func executeTwoGETsForChecksumValidation(proxyURL string, bck cmn.Bck, objName string, t *testing.T) {
	baseParams := tutils.BaseAPIParams(proxyURL)
	_, err := api.GetObjectWithValidation(baseParams, bck, objName)
	if err == nil {
		t.Error("Error is nil, expected internal server error on a GET for an object")
	} else if !strings.Contains(err.Error(), "500") {
		t.Errorf("Expected internal server error on a GET for a corrupted object, got [%s]", err.Error())
	}
	// Execute another GET to make sure that the object is deleted
	_, err = api.GetObjectWithValidation(baseParams, bck, objName)
	if err == nil {
		t.Error("Error is nil, expected not found on a second GET for a corrupted object")
	} else if !strings.Contains(err.Error(), "404") {
		t.Errorf("Expected Not Found on a second GET for a corrupted object, got [%s]", err.Error())
	}
}

// TODO: validate range checksums
func TestRangeRead(t *testing.T) {
	initMountpaths(t, tutils.RandomProxyURL(t)) // to run findObjOnDisk() and validate range

	runProviderTests(t, func(t *testing.T, bck *cluster.Bck) {
		var (
			m = ioContext{
				t:         t,
				bck:       bck.Bck,
				num:       5,
				fileSize:  5271,
				fixedSize: true,
			}
			proxyURL   = tutils.RandomProxyURL(t)
			baseParams = tutils.BaseAPIParams(proxyURL)
			cksumProps = bck.CksumConf()
		)

		m.init()
		m.puts()
		if m.bck.IsRemote() {
			defer m.del()
		}
		objName := m.objNames[0]

		defer func() {
			tlog.Logln("Cleaning up...")
			propsToUpdate := &cmn.BucketPropsToUpdate{
				Cksum: &cmn.CksumConfToUpdate{
					EnableReadRange: api.Bool(cksumProps.EnableReadRange),
				},
			}
			_, err := api.SetBucketProps(baseParams, bck.Bck, propsToUpdate)
			tassert.CheckError(t, err)
		}()

		tlog.Logln("Valid range with object checksum...")
		// Validate that entire object checksum is being returned
		if cksumProps.EnableReadRange {
			propsToUpdate := &cmn.BucketPropsToUpdate{
				Cksum: &cmn.CksumConfToUpdate{
					EnableReadRange: api.Bool(false),
				},
			}
			_, err := api.SetBucketProps(baseParams, bck.Bck, propsToUpdate)
			tassert.CheckFatal(t, err)
		}
		testValidCases(t, proxyURL, bck.Bck, cksumProps.Type, m.fileSize, objName, true)

		// Validate only that range checksum is being returned
		tlog.Logln("Valid range with range checksum...")
		if !cksumProps.EnableReadRange {
			propsToUpdate := &cmn.BucketPropsToUpdate{
				Cksum: &cmn.CksumConfToUpdate{
					EnableReadRange: api.Bool(true),
				},
			}
			_, err := api.SetBucketProps(baseParams, bck.Bck, propsToUpdate)
			tassert.CheckFatal(t, err)
		}
		testValidCases(t, proxyURL, bck.Bck, cksumProps.Type, m.fileSize, objName, false)

		tlog.Logln("Valid range query...")
		verifyValidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=-1", 1)
		verifyValidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=0-", int64(m.fileSize))
		verifyValidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=10-", int64(m.fileSize-10))
		verifyValidRangesQuery(t, proxyURL, bck.Bck, objName, fmt.Sprintf("bytes=-%d", m.fileSize), int64(m.fileSize))
		verifyValidRangesQuery(t, proxyURL, bck.Bck, objName, fmt.Sprintf("bytes=-%d", m.fileSize+2), int64(m.fileSize))

		tlog.Logln("======================================================================")
		tlog.Logln("Invalid range query:")
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, "potatoes=0-1")
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes")
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, fmt.Sprintf("bytes=%d-", m.fileSize+1))
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, fmt.Sprintf("bytes=%d-%d", m.fileSize+1, m.fileSize+2))
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=0--1")
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=1-0")
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=-1-0")
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=-1-2")
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=10--1")
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=1-2,4-6")
		verifyInvalidRangesQuery(t, proxyURL, bck.Bck, objName, "bytes=--1")
	})
}

// TODO: validate range checksum if enabled
func testValidCases(t *testing.T, proxyURL string, bck cmn.Bck, cksumType string, fileSize uint64, objName string,
	checkEntireObjCksum bool) {
	// Range-Read the entire file in 500 increments
	byteRange := int64(500)
	iterations := int64(fileSize) / byteRange
	for i := int64(0); i < iterations; i += byteRange {
		verifyValidRanges(t, proxyURL, bck, cksumType, objName, i, byteRange, byteRange, checkEntireObjCksum)
	}

	verifyValidRanges(t, proxyURL, bck, cksumType, objName, byteRange*iterations, byteRange,
		int64(fileSize)%byteRange, checkEntireObjCksum)
}

func verifyValidRanges(t *testing.T, proxyURL string, bck cmn.Bck, cksumType, objName string,
	offset, length, expectedLength int64, checkEntireObjCksum bool) {
	var (
		w          = bytes.NewBuffer(nil)
		hdr        = cmn.RangeHdr(offset, length)
		baseParams = tutils.BaseAPIParams(proxyURL)
		fqn        = findObjOnDisk(bck, objName)
		options    = api.GetObjectInput{Writer: w, Header: hdr}
	)
	n, err := api.GetObjectWithValidation(baseParams, bck, objName, options)
	if err != nil {
		if !checkEntireObjCksum {
			t.Errorf("Failed to get object %s/%s! Error: %v", bck, objName, err)
		} else {
			if ckErr, ok := err.(*cmn.ErrInvalidCksum); ok {
				file, err := os.Open(fqn)
				if err != nil {
					t.Fatalf("Unable to open file: %s. Error:  %v", fqn, err)
				}
				defer file.Close()
				_, cksum, err := cos.CopyAndChecksum(io.Discard, file, nil, cksumType)
				if err != nil {
					t.Errorf("Unable to compute cksum of file: %s. Error:  %s", fqn, err)
				}
				if cksum.Value() != ckErr.Expected() {
					t.Errorf("Expected entire object checksum [%s], checksum returned in response [%s]",
						ckErr.Expected(), cksum)
				}
			} else {
				t.Errorf("Unexpected error returned [%v].", err)
			}
		}
	} else if n != expectedLength {
		t.Errorf("number of bytes received is different than expected (expected: %d, got: %d)", expectedLength, n)
	}

	file, err := os.Open(fqn)
	if err != nil {
		t.Fatalf("Unable to open file: %s. Error:  %v", fqn, err)
	}
	defer file.Close()
	outputBytes := w.Bytes()
	sectionReader := io.NewSectionReader(file, offset, length)
	expectedBytesBuffer := bytes.NewBuffer(nil)
	_, err = expectedBytesBuffer.ReadFrom(sectionReader)
	if err != nil {
		t.Errorf("Unable to read the file %s, from offset: %d and length: %d. Error: %v", fqn, offset, length, err)
	}
	expectedBytes := expectedBytesBuffer.Bytes()
	if len(outputBytes) != len(expectedBytes) {
		t.Errorf("Bytes length mismatch. Expected bytes: [%d]. Actual bytes: [%d]", len(expectedBytes), len(outputBytes))
	}
	if int64(len(outputBytes)) != expectedLength {
		t.Errorf("Returned bytes don't match expected length. Expected length: [%d]. Output length: [%d]",
			expectedLength, len(outputBytes))
	}
	for i := 0; i < len(expectedBytes); i++ {
		if expectedBytes[i] != outputBytes[i] {
			t.Errorf("Byte mismatch. Expected: %v, Actual: %v", string(expectedBytes), string(outputBytes))
		}
	}
}

func verifyValidRangesQuery(t *testing.T, proxyURL string, bck cmn.Bck, objName, rangeQuery string, expectedLength int64) {
	var (
		baseParams = tutils.BaseAPIParams(proxyURL)
		hdr        = http.Header{cmn.HdrRange: {rangeQuery}}
		options    = api.GetObjectInput{Header: hdr}
	)
	resp, n, err := api.GetObjectWithResp(baseParams, bck, objName, options) // nolint:bodyclose // it's closed inside
	tassert.CheckFatal(t, err)
	tassert.Errorf(
		t, resp.ContentLength == expectedLength,
		"number of bytes received is different than expected (expected: %d, got: %d)",
		expectedLength, resp.ContentLength,
	)
	tassert.Errorf(
		t, n == expectedLength,
		"number of bytes received is different than expected (expected: %d, got: %d)",
		expectedLength, n,
	)
	acceptRanges := resp.Header.Get(cmn.HdrAcceptRanges)
	tassert.Errorf(
		t, acceptRanges == "bytes",
		"%q header is not set correctly: %s", cmn.HdrAcceptRanges, acceptRanges,
	)
	contentRange := resp.Header.Get(cmn.HdrContentRange)
	tassert.Errorf(t, contentRange != "", "%q header should be set", cmn.HdrContentRange)
}

func verifyInvalidRangesQuery(t *testing.T, proxyURL string, bck cmn.Bck, objName, rangeQuery string) {
	var (
		baseParams = tutils.BaseAPIParams(proxyURL)
		hdr        = http.Header{cmn.HdrRange: {rangeQuery}}
		options    = api.GetObjectInput{Header: hdr}
	)
	_, err := api.GetObjectWithValidation(baseParams, bck, objName, options)
	tassert.Errorf(t, err != nil, "must fail for %q combination", rangeQuery)
}

func Test_checksum(t *testing.T) {
	var (
		m = ioContext{
			t:        t,
			bck:      cliBck,
			num:      5,
			fileSize: largeFileSize,
		}
		totalSize  = int64(uint64(m.num) * m.fileSize)
		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
	)

	tutils.CheckSkip(t, tutils.SkipTestArgs{Long: true, RemoteBck: true, Bck: m.bck})

	p, err := api.HeadBucket(baseParams, m.bck)
	tassert.CheckFatal(t, err)

	t.Cleanup(func() {
		propsToUpdate := &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				Type:            api.String(p.Cksum.Type),
				ValidateColdGet: api.Bool(p.Cksum.ValidateColdGet),
			},
		}
		_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckError(t, err)
		m.del()
	})

	m.remotePuts(true /*evict*/)

	// Disable checkum.
	if p.Cksum.Type != cos.ChecksumNone {
		propsToUpdate := &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				Type: api.String(cos.ChecksumNone),
			},
		}
		_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckFatal(t, err)
	}

	// Disable cold get validation.
	if p.Cksum.ValidateColdGet {
		propsToUpdate := &cmn.BucketPropsToUpdate{
			Cksum: &cmn.CksumConfToUpdate{
				ValidateColdGet: api.Bool(false),
			},
		}
		_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
		tassert.CheckFatal(t, err)
	}

	start := time.Now()
	m.gets(false /*withValidate*/)
	tlog.Logf("GET %s without any checksum validation: %v\n", cos.B2S(totalSize, 0), time.Since(start))

	m.evict()

	propsToUpdate := &cmn.BucketPropsToUpdate{
		Cksum: &cmn.CksumConfToUpdate{
			Type:            api.String(cos.ChecksumXXHash),
			ValidateColdGet: api.Bool(true),
		},
	}
	_, err = api.SetBucketProps(baseParams, m.bck, propsToUpdate)
	tassert.CheckFatal(t, err)

	start = time.Now()
	m.gets(true /*withValidate*/)
	tlog.Logf("GET %s and validate checksum: %v\n", cos.B2S(totalSize, 0), time.Since(start))
}

func validateBucketProps(t *testing.T, expected *cmn.BucketPropsToUpdate, actual *cmn.BucketProps) {
	// Apply changes on props that we have received. If after applying anything
	// has changed it means that the props were not applied.
	tmpProps := actual.Clone()
	tmpProps.Apply(expected)
	if !reflect.DeepEqual(tmpProps, actual) {
		t.Errorf("bucket props are not equal, expected: %+v, got: %+v", tmpProps, actual)
	}
}

func resetBucketProps(t *testing.T, proxyURL string, bck cmn.Bck) {
	baseParams := tutils.BaseAPIParams(proxyURL)
	_, err := api.ResetBucketProps(baseParams, bck)
	tassert.CheckError(t, err)
}

func corruptSingleBitInFile(t *testing.T, bck cmn.Bck, objName string) {
	var (
		fqn     = findObjOnDisk(bck, objName)
		fi, err = os.Stat(fqn)
		b       = []byte{0}
	)
	tassert.CheckFatal(t, err)
	off := rand.Int63n(fi.Size())
	file, err := os.OpenFile(fqn, os.O_RDWR, cos.PermRWR)
	tassert.CheckFatal(t, err)
	_, err = file.Seek(off, 0)
	tassert.CheckFatal(t, err)
	_, err = file.Read(b)
	tassert.CheckFatal(t, err)
	bit := rand.Intn(8)
	b[0] ^= 1 << bit
	_, err = file.Seek(off, 0)
	tassert.CheckFatal(t, err)
	_, err = file.Write(b)
	tassert.CheckFatal(t, err)
	file.Close()
}

func TestPutObjectWithChecksum(t *testing.T) {
	var (
		proxyURL   = tutils.RandomProxyURL(t)
		baseParams = tutils.BaseAPIParams(proxyURL)
		bckLocal   = cmn.Bck{
			Name:     cliBck.Name,
			Provider: cmn.ProviderAIS,
		}
		basefileName = "mytestobj.txt"
		objData      = []byte("I am object data")
		badCksumVal  = "badchecksum"
	)
	tutils.CreateFreshBucket(t, proxyURL, bckLocal, nil)
	putArgs := api.PutObjectArgs{
		BaseParams: baseParams,
		Bck:        bckLocal,
		Reader:     readers.NewBytesReader(objData),
	}
	for _, cksumType := range cos.SupportedChecksums() {
		if cksumType == cos.ChecksumNone {
			continue
		}
		fileName := basefileName + cksumType
		hasher := cos.NewCksumHash(cksumType)
		hasher.H.Write(objData)
		cksumValue := hex.EncodeToString(hasher.H.Sum(nil))
		putArgs.Cksum = cos.NewCksum(cksumType, badCksumVal)
		putArgs.Object = fileName
		err := api.PutObject(putArgs)
		if err == nil {
			t.Errorf("Bad checksum provided by the user, Expected an error")
		}
		_, err = api.HeadObject(baseParams, bckLocal, fileName)
		if err == nil || !strings.Contains(err.Error(), strconv.Itoa(http.StatusNotFound)) {
			t.Errorf("Object %s exists despite bad checksum", fileName)
		}
		putArgs.Cksum = cos.NewCksum(cksumType, cksumValue)
		err = api.PutObject(putArgs)
		if err != nil {
			t.Errorf("Correct checksum provided, Err encountered %v", err)
		}
		_, err = api.HeadObject(baseParams, bckLocal, fileName)
		if err != nil {
			t.Errorf("Object %s does not exist despite correct checksum", fileName)
		}
	}
}

func TestOperationsWithRanges(t *testing.T) {
	const (
		objCnt  = 50 // NOTE: must by a multiple of 10
		objSize = cos.KiB
	)
	proxyURL := tutils.RandomProxyURL(t)

	runProviderTests(t, func(t *testing.T, bck *cluster.Bck) {
		for _, evict := range []bool{false, true} {
			t.Run(fmt.Sprintf("evict=%t", evict), func(t *testing.T) {
				if evict && bck.IsAIS() {
					t.Skip("operation `evict` is not applicable to AIS buckets")
				}

				var (
					objList   = make([]string, 0, objCnt)
					cksumType = bck.Props.Cksum.Type
				)

				for i := 0; i < objCnt/2; i++ {
					objList = append(objList,
						fmt.Sprintf("test/a-%04d", i),
						fmt.Sprintf("test/b-%04d", i),
					)
				}
				for _, objName := range objList {
					r, _ := readers.NewRandReader(objSize, cksumType)
					err := api.PutObject(api.PutObjectArgs{
						BaseParams: baseParams,
						Bck:        bck.Bck,
						Object:     objName,
						Reader:     r,
						Size:       objSize,
					})
					tassert.CheckFatal(t, err)
				}

				tests := []struct {
					// Title to print out while testing.
					name string
					// A range of objects.
					rangeStr string
					// Total number of objects expected to be deleted/evicted.
					delta int
				}{
					{
						"Trying to delete/evict objects with invalid prefix",
						"file/a-{0..10}",
						0,
					},
					{
						"Trying to delete/evict objects out of range",
						"test/a-" + fmt.Sprintf("{%d..%d}", objCnt+10, objCnt+110),
						0,
					},
					{
						fmt.Sprintf("Deleting/Evicting %d objects with prefix 'a-'", objCnt/10),
						"test/a-" + fmt.Sprintf("{%04d..%04d}", (objCnt-objCnt/5)/2, objCnt/2),
						objCnt / 10,
					},
					{
						fmt.Sprintf("Deleting/Evicting %d objects (short range)", objCnt/5),
						"test/b-" + fmt.Sprintf("{%04d..%04d}", 1, objCnt/5),
						objCnt / 5,
					},
					{
						"Deleting/Evicting objects with empty range",
						"test/b-",
						objCnt/2 - objCnt/5,
					},
				}

				var (
					totalFiles  = objCnt
					baseParams  = tutils.BaseAPIParams(proxyURL)
					waitTimeout = 10 * time.Second
				)

				if bck.IsRemote() {
					waitTimeout = 40 * time.Second
				}

				for idx, test := range tests {
					tlog.Logf("%d. %s; range: [%s]\n", idx+1, test.name, test.rangeStr)

					var (
						err    error
						xactID string
						kind   string
						msg    = &cmn.SelectMsg{Prefix: "test/"}
					)
					if evict {
						xactID, err = api.EvictRange(baseParams, bck.Bck, test.rangeStr)
						msg.Flags = cmn.SelectCached
						kind = cmn.ActEvictObjects
					} else {
						xactID, err = api.DeleteRange(baseParams, bck.Bck, test.rangeStr)
						kind = cmn.ActDeleteObjects
					}
					if err != nil {
						t.Error(err)
						continue
					}

					args := api.XactReqArgs{ID: xactID, Kind: kind, Timeout: waitTimeout}
					_, err = api.WaitForXaction(baseParams, args)
					tassert.CheckFatal(t, err)

					totalFiles -= test.delta
					objList, err := api.ListObjects(baseParams, bck.Bck, msg, 0)
					if err != nil {
						t.Error(err)
						continue
					}
					if len(objList.Entries) != totalFiles {
						t.Errorf("Incorrect number of remaining objects: %d, should be %d", len(objList.Entries), totalFiles)
						continue
					}

					tlog.Logf("  %d objects have been deleted/evicted\n", test.delta)
				}

				msg := &cmn.SelectMsg{Prefix: "test/"}
				bckList, err := api.ListObjects(baseParams, bck.Bck, msg, 0)
				tassert.CheckFatal(t, err)

				tlog.Logf("Cleaning up remaining objects...\n")
				for _, obj := range bckList.Entries {
					err := tutils.Del(proxyURL, bck.Bck, obj.Name, nil, nil, true /*silent*/)
					tassert.CheckError(t, err)
				}
			})
		}
	})
}
