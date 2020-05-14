// Package api provides RESTful API to AIS object storage
/*
 * Copyright (c) 2018, NVIDIA CORPORATION. All rights reserved.
 */
package api

import (
	"errors"
	"net/http"

	"github.com/NVIDIA/aistore/cmn"
	jsoniter "github.com/json-iterator/go"
)

type AuthnSpec struct {
	AdminName     string
	AdminPassword string
	UserName      string
	UserPassword  string
}

type ClusterSpec struct {
	AdminName     string
	AdminPassword string
	ClusterID     string
	URLs          []string
}

type userRec struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type loginRec struct {
	Password string `json:"password"`
}

type AuthCreds struct {
	Token string `json:"token"`
}

type ClusterRec struct {
	Conf map[string][]string `json:"conf"`
}

func AddUser(baseParams BaseParams, spec AuthnSpec) error {
	req := userRec{Name: spec.UserName, Password: spec.UserPassword}
	msg, err := jsoniter.Marshal(req)
	if err != nil {
		return err
	}

	baseParams.Method = http.MethodPost
	return DoHTTPRequest(ReqParams{
		BaseParams: baseParams,
		Path:       cmn.URLPath(cmn.Version, cmn.Users),
		Body:       msg,
		User:       spec.AdminName,
		Password:   spec.AdminPassword,
	})
}

func DeleteUser(baseParams BaseParams, spec AuthnSpec) error {
	baseParams.Method = http.MethodDelete
	return DoHTTPRequest(ReqParams{
		BaseParams: baseParams,
		Path:       cmn.URLPath(cmn.Version, cmn.Users, spec.UserName),
		User:       spec.AdminName,
		Password:   spec.AdminPassword,
	})
}

func LoginUser(baseParams BaseParams, spec AuthnSpec) (token *AuthCreds, err error) {
	baseParams.Method = http.MethodPost

	err = DoHTTPRequest(ReqParams{
		BaseParams: baseParams,
		Path:       cmn.URLPath(cmn.Version, cmn.Users, spec.UserName),
		Body:       cmn.MustMarshal(loginRec{Password: spec.UserPassword}),
		User:       spec.AdminName,
		Password:   spec.AdminPassword,
	}, &token)
	if err != nil {
		return nil, err
	}

	if token.Token == "" {
		return nil, errors.New("login failed: empty response from AuthN server")
	}
	return token, nil
}

func RegisterClusterAuthN(baseParams BaseParams, spec ClusterSpec) error {
	req := ClusterRec{Conf: make(map[string][]string, 1)}
	req.Conf[spec.ClusterID] = spec.URLs
	msg, err := jsoniter.Marshal(req)
	if err != nil {
		return err
	}

	baseParams.Method = http.MethodPost
	return DoHTTPRequest(ReqParams{
		BaseParams: baseParams,
		Path:       cmn.URLPath(cmn.Version, cmn.Clusters),
		Body:       msg,
		User:       spec.AdminName,
		Password:   spec.AdminPassword,
	})
}

func UnregisterClusterAuthN(baseParams BaseParams, spec ClusterSpec) error {
	baseParams.Method = http.MethodDelete
	return DoHTTPRequest(ReqParams{
		BaseParams: baseParams,
		Path:       cmn.URLPath(cmn.Version, cmn.Clusters, spec.ClusterID),
		User:       spec.AdminName,
		Password:   spec.AdminPassword,
	})
}

func GetClusterAuthN(baseParams BaseParams, spec ClusterSpec) (*ClusterRec, error) {
	baseParams.Method = http.MethodGet
	path := cmn.URLPath(cmn.Version, cmn.Clusters)
	if spec.ClusterID != "" {
		path = cmn.URLPath(path, spec.ClusterID)
	}
	clusters := &ClusterRec{}
	err := DoHTTPRequest(ReqParams{
		BaseParams: baseParams,
		Path:       path,
	}, clusters)
	return clusters, err
}
