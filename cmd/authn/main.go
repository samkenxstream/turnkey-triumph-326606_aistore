// Package main - authorization server for AIStore. See README.md for more info.
/*
 * Copyright (c) 2018, NVIDIA CORPORATION. All rights reserved.
 *
 */
package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/NVIDIA/aistore/3rdparty/glog"
	"github.com/NVIDIA/aistore/cmn"
	"github.com/NVIDIA/aistore/cmn/jsp"
)

var (
	version, build string
	configPath     string
	conf           = &config{Cluster: clusterConfig{Conf: make(map[string][]string)}}
)

// Set up glog with options from configuration file
func updateLogOptions() error {
	err := flag.Lookup("log_dir").Value.Set(conf.Log.Dir)
	if err != nil {
		return fmt.Errorf("failed to flag-set glog dir %q, err: %v", conf.Log.Dir, err)
	}
	if err = cmn.CreateDir(conf.Log.Dir); err != nil {
		return fmt.Errorf("failed to create log dir %q, err: %v", conf.Log.Dir, err)
	}

	if conf.Log.Level != "" {
		v := flag.Lookup("v").Value
		if v == nil {
			return fmt.Errorf("nil -v Value")
		}
		if err = v.Set(conf.Log.Level); err != nil {
			return fmt.Errorf("failed to set log level = %s, err: %v", conf.Log.Level, err)
		}
	}
	return nil
}

func main() {
	fmt.Printf("version: %s | build_time: %s\n", version, build)

	var (
		err error
	)

	flag.Parse()
	confFlag := flag.Lookup("config")
	if confFlag != nil {
		configPath = confFlag.Value.String()
	}

	if configPath == "" {
		glog.Fatalf("Missing configuration file")
	}

	if glog.V(4) {
		glog.Infof("Reading configuration from %s", configPath)
	}
	if err = jsp.Load(configPath, conf, jsp.Plain()); err != nil {
		glog.Fatalf("Failed to load configuration: %v", err)
	}
	conf.path = configPath
	conf.applySecrets()
	if err = conf.validate(); err != nil {
		glog.Fatalf("Invalid configuration: %v", err)
	}

	if err = updateLogOptions(); err != nil {
		glog.Fatalf("Failed to set up logger: %v", err)
	}

	dbPath := filepath.Join(conf.ConfDir, userListFile)
	srv := newAuthServ(newUserManager(dbPath))
	if err := srv.run(); err != nil {
		glog.Fatalf(err.Error())
	}
}
