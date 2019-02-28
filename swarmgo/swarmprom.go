/*
 * Copyright (c) 2018-present unTill Pro, Ltd. and Contributors
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 *
 */

package swarmgo

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tmc/scp"
	"golang.org/x/crypto/ssh"
)

const (
	swarmgoPrefix            = "swarmgo/"
	swarmpromFolder          = "swarmprom"
	swarmpromComposeFileName = swarmpromFolder + "/swarmprom.yml"
	alertmanagerConfigPath   = swarmpromFolder + "/alertmanager/alertmanager.yml"
)

type infoForCopy struct {
	nodeEntry   *entry
	config      *ssh.ClientConfig
	clusterFile *clusterFile
}

var swarmpromCmd = &cobra.Command{
	Use:   "swarmprom",
	Short: "Create starter kit for swarm monitoring",
	Long:  `Deploys Prometheus, WebhookURL, cAdvisor, Node Exporter, Alert Manager and Unsee to the current swarm`,
	Run: func(cmd *cobra.Command, args []string) {
		if logs {
			f := redirectLogs()
			defer func() {
				if err := f.Close(); err != nil {
					log.Println("Error closing the file: ", err.Error())
				}
			}()
		}
		passToKey := readKeyPassword()
		firstEntry, clusterFile := getSwarmLeaderNodeAndClusterFile()
		if !firstEntry.node.Traefik {
			log.Fatal("Need to deploy traefik before swarmprom deploy")
		}
		deploySwarmprom(passToKey, clusterFile, firstEntry)
	},
}

func deploySwarmprom(passToKey string, clusterFile *clusterFile, firstEntry *entry) {
	clusterFile.GrafanaPassword = readPasswordPrompt("Grafana admin user password")
	fmt.Println("Enter webhook URL for alertmanager")
	clusterFile.WebhookURL = waitUserInput()
	//TODO don't forget to implement passwords for prometheus and traefik
	host := firstEntry.node.Host
	config := findSSHKeysAndInitConnection(passToKey, clusterFile)
	forCopy := infoForCopy{
		firstEntry,
		config,
		clusterFile,
	}
	log.Println("Trying to install dos2unix")
	sudoExecSSHCommand(host, "apt-get install dos2unix", config)
	curDir := getCurrentDir()
	copyToHost(&forCopy, filepath.ToSlash(filepath.Join(curDir, swarmpromFolder)))
	filesToApplyTemplate := [2]string{alertmanagerConfigPath, swarmpromComposeFileName}
	for _, fileToApplyTemplate := range filesToApplyTemplate {
		appliedBuffer := applyExecutorToTemplateFile(fileToApplyTemplate, clusterFile)
		execSSHCommand(host, "cat > ~/"+fileToApplyTemplate+" << EOF\n\n"+
			appliedBuffer.String()+"\nEOF", config)
		log.Println(fileToApplyTemplate, "applied by template")
	}
	log.Println("Trying to deploy swarmprom")
	sudoExecSSHCommand(host, "docker stack deploy -c "+swarmpromComposeFileName+" prom", config)
	log.Println("Swarmprom deployed")
}

func copyToHost(forCopy *infoForCopy, src string) {
	info, err := os.Lstat(src)
	CheckErr(err)
	if info.IsDir() {
		copyDirToHost(src, forCopy)
	} else {
		copyFileToHost(src, forCopy)
	}
}

func copyDirToHost(dirPath string, forCopy *infoForCopy) {
	execSSHCommand(forCopy.nodeEntry.node.Host, "mkdir -p "+substringAfter(dirPath, swarmgoPrefix), forCopy.config)
	dirContent, err := ioutil.ReadDir(dirPath)
	CheckErr(err)
	for _, dirEntry := range dirContent {
		src := filepath.ToSlash(filepath.Join(dirPath, dirEntry.Name()))
		copyToHost(forCopy, src)
	}
}

func copyFileToHost(filePath string, forCopy *infoForCopy) {
	host := forCopy.nodeEntry.node.Host
	relativePath := substringAfter(filePath, swarmgoPrefix)
	err := scp.CopyPath(filePath, relativePath, getSSHSession(host, forCopy.config))
	sudoExecSSHCommand(forCopy.nodeEntry.node.Host, "dos2unix "+relativePath, forCopy.config)
	sudoExecSSHCommand(host, "chown root:root "+relativePath, forCopy.config)
	sudoExecSSHCommand(host, "chmod 777 "+relativePath, forCopy.config)
	CheckErr(err)
	log.Println(relativePath, "copied on host")
}