// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func (s *Zuite) TestCollectPackages() {
	var defs defs
	deps, err := defs.collectPackages(s.cwd)
	require.NoError(s.T(), err)

	// Check dependency graph.

	require.Len(s.T(), deps, 4)

	sample_deps := deps[p("sample_deps")]
	require.NotNil(s.T(), sample_deps)
	require.Len(s.T(), sample_deps.dependsOn, 2)
	require.NotNil(s.T(), sample_deps.dependsOn[p("sample_deps/a")])
	require.NotNil(s.T(), sample_deps.dependsOn[p("sample_deps/b")])

	a := deps[p("sample_deps/a")]
	require.NotNil(s.T(), a)
	require.Len(s.T(), a.dependsOn, 1)
	require.NotNil(s.T(), a.dependsOn["fmt"])

	b := deps[p("sample_deps/b")]
	require.NotNil(s.T(), b)
	require.Len(s.T(), b.dependsOn, 1)
	require.NotNil(s.T(), sample_deps.dependsOn[p("sample_deps/a")])

	fmtpkg := deps["fmt"]
	require.NotNil(s.T(), fmtpkg)
	require.Len(s.T(), fmtpkg.dependsOn, 0)

	// Check goroot'ness.

	require.False(s.T(), deps[p("sample_deps")].goroot)
	require.False(s.T(), deps[p("sample_deps/a")].goroot)
	require.False(s.T(), deps[p("sample_deps/b")].goroot)
	require.True(s.T(), deps["fmt"].goroot)
}

type Zuite struct {
	suite.Suite
	cwd string
}

func TestRunAllTheTests(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	s := new(Zuite)
	s.cwd = cwd + "/sample_deps"
	suite.Run(t, s)
}

func p(name string) string {
	return fmt.Sprintf("github.com/helloeave/depper/%s", name)
}
