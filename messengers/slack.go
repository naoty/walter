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
package messengers

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/recruit-tech/walter/log"
)

type Slack struct {
	Channel   string   `config:"channel" json:"channel"`
	UserName  string   `config:"username" json:"username"`
	IconEmoji string   `config:"icon" json:"icon_emoji,omitempty"`
	IncomingUrl string `config:"url" json:"-"` // not map to json
}

// To avoid the infinite recursion
// (see http://stackoverflow.com/questions/23045884/can-i-use-marshaljson-to-add-arbitrary-fields-to-a-json-encoding-in-golang)
type FakeSlack Slack

func (self *Slack) Post(message string) bool {
	params, _ := json.Marshal(struct {
		FakeSlack
		Text string `json:"text"`
	}{
		FakeSlack : FakeSlack(*self),
		Text:  message,
	})

	resp, _ := http.PostForm(
		self.IncomingUrl,
		url.Values{"payload": {string(params)}},
	)

	body, _ := ioutil.ReadAll(resp.Body)
	log.Infof("Post result...: %s", body)
	defer resp.Body.Close()

	return true
}
