/* walter: a deployment pipeline template
* Copyright (C) 2014 Recruit Technologies Co., Ltd. and contributors
* (see CONTRIBUTORS.md)
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
* http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */
package walter

import (
	"os/exec"
	"reflect"
	"strconv"
	"time"

	"github.com/google/go-github/github"
	"github.com/recruit-tech/walter/config"
	"github.com/recruit-tech/walter/engine"
	"github.com/recruit-tech/walter/log"
	"github.com/recruit-tech/walter/services"
	"github.com/recruit-tech/walter/stages"
)

type Walter struct {
	Engine *engine.Engine
	Opts   *config.Opts
}

func New(opts *config.Opts) (*Walter, error) {
	configData := config.ReadConfig(opts.PipelineFilePath)
	pipeline, err := config.Parse(configData)
	if err != nil {
		return nil, err
	}
	monitorCh := make(chan stages.Mediator)
	engine := &engine.Engine{
		Pipeline:  pipeline,
		Opts:      opts,
		MonitorCh: &monitorCh,
	}
	return &Walter{
		Opts:   opts,
		Engine: engine,
	}, err
}

func (e *Walter) Run() bool {
	repoServiceValue := reflect.ValueOf(e.Engine.Pipeline.RepoService)
	log.Info(repoServiceValue.Type().String())
	if e.Engine.Opts.Mode == "local" ||
		repoServiceValue.Type().String() == "*services.LocalClient" {
		log.Info("Starting Walter in local mode")
		mediator := e.Engine.RunOnce()
		return !mediator.IsAnyFailure()
	} else {
		log.Info("Starting Walter in repository service mode")
		return e.runService()
	}
}

func (e *Walter) runService() bool {
	// load .walter-update
	log.Infof("Loading update file... \"%s\"", e.Engine.Pipeline.RepoService.GetUpdateFilePath())
	update, err := services.LoadLastUpdate(e.Engine.Pipeline.RepoService.GetUpdateFilePath())
	log.Infof("Succeeded loading update file")

	log.Info("Updating status...")
	update.Status = "inprogress"
	result := services.SaveLastUpdate(e.Engine.Pipeline.RepoService.GetUpdateFilePath(), update)
	if result == false {
		log.Error("Failed to save status update")
		return false
	}
	log.Info("Succeeded updating status")

	// get latest commit and pull requests
	log.Info("downloading commits and pull requests...")
	commits, err := e.Engine.Pipeline.RepoService.GetCommits(update)
	if err != nil {
		log.Errorf("Failed getting commits: %s", err)
		return false
	}

	log.Info("Succeeded getting commits")
	for commit := commits.Front(); commit != nil; commit = commit.Next() {
		commitType := reflect.TypeOf(commit.Value)
		if commitType.Name() == "RepositoryCommit" {
			log.Info("Found new repository commit")
			trunkCommit := commit.Value.(github.RepositoryCommit)
			e.processTrunkCommit(trunkCommit)
		} else if commitType.Name() == "PullRequest" {
			log.Info("Found new pull request commit")
			pullreq := commit.Value.(github.PullRequest)
			if result := e.processPullRequest(pullreq); result == false {
				return false
			}
		} else {
			log.Errorf("Nothing commit type: %s", commitType)
		}
	}

	// save .walter-update
	log.Info("Saving update file...")
	update.Status = "finished"
	update.Time = time.Now()
	result = services.SaveLastUpdate(e.Engine.Pipeline.RepoService.GetUpdateFilePath(), update)
	if result == false {
		log.Error("Failed to save update")
		return false
	}
	return true
}

func (e *Walter) processTrunkCommit(commit github.RepositoryCommit) bool {
	log.Infof("Checkout master branch")
	_, err := exec.Command("git", "checkout", "master", "-f").Output()
	if err != nil {
		log.Errorf("Failed to checkout master branch: %s", err)
		return false
	}
	log.Infof("Downloading new commit from master")
	_, err = exec.Command("git", "pull", "origin", "master").Output()
	if err != nil {
		log.Errorf("Failed to download new commit from master: %s", err)
		return false
	}
	log.Infof("Running the latest commit in master")
	w, _ := New(e.Opts)
	mediator := w.Engine.RunOnce()

	// register the result to hosting service
	if mediator.IsAnyFailure() {
		log.Error("Error reported...")
		e.Engine.Pipeline.RepoService.RegisterResult(
			services.Result{
				State:   "failure",
				Message: "Failed running pipleline ...",
				SHA:     *commit.SHA})
		return false
	} else {
		log.Info("Succeeded.")
		e.Engine.Pipeline.RepoService.RegisterResult(
			services.Result{
				State:   "success",
				Message: "Succeeded running pipeline...",
				SHA:     *commit.SHA})
		return true
	}
}

func (e *Walter) processPullRequest(pullrequest github.PullRequest) bool {
	// checkout pullrequest
	num := *pullrequest.Number
	_, err := exec.Command("git", "fetch", "origin", "refs/pull/"+strconv.Itoa(num)+"/head:pr_"+strconv.Itoa(num)).Output()

	defer exec.Command("git", "checkout", "master", "-f").Output() // TODO: make trunk branch configurable
	defer log.Info("returning master branch...")

	if err != nil {
		log.Errorf("Failed to fetch pull request: %s", err)
		return false
	}

	_, err = exec.Command("git", "checkout", "pr_"+strconv.Itoa(num)).Output()
	if err != nil {
		log.Errorf("Failed to checkout pullrequest branch (\"%s\") : %s", "pr_"+strconv.Itoa(num), err)
		return false
	}

	// run pipeline
	log.Info("Running pipeline...")
	w, _ := New(e.Opts)
	mediator := w.Engine.RunOnce()

	// register the result to hosting service
	if mediator.IsAnyFailure() {
		log.Error("Error reported...")
		e.Engine.Pipeline.RepoService.RegisterResult(
			services.Result{
				State:   "failure",
				Message: "Failed running pipleline ...",
				SHA:     *pullrequest.Head.SHA})
		return false
	} else {
		log.Info("succeeded.")
		e.Engine.Pipeline.RepoService.RegisterResult(
			services.Result{
				State:   "success",
				Message: "Succeeded running pipeline...",
				SHA:     *pullrequest.Head.SHA})
		return true
	}
}
