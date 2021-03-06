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
	"io/ioutil"
	"os"
	"regexp"
	"runtime"
	"strings"

	"golang.org/x/tools/go/packages"
	"gopkg.in/yaml.v2"
)

type defs struct {
	Config struct {
		WorkingPackage string `yaml:"working_package"`
	} `yaml:"config"`
	Rules []*rule `yaml:"rules"`
}

type rule struct {
	Name      string   `yaml:"name"`
	Packages  string   `yaml:"packages"`
	MayDepend []string `yaml:"may_depend"`
	Expected  []string `yaml:"deprecated_dependencies"`

	// fields denormalized on parse
	packagePattern           *regexp.Regexp
	mayDepends               []*pkgpattern
	expectedStarToPackage    map[string]bool
	expectedPackageToPackage map[string]map[string]bool

	// violations are gathered during rule processing
	actualPackagesProcessed map[string]bool
	violations              []string
}

type pkg struct {
	name      string
	goroot    bool
	dependsOn map[string]*pkg
}

func (pkg *pkg) String() string {
	if pkg.goroot {
		return fmt.Sprintf("<%s>", pkg.name)
	} else {
		return pkg.name
	}
}

// pkgpattern represents a pattern of packages, which you can match a specific
// package against.
type pkgpattern struct {
	goroot         bool
	thirdParties   bool
	workingPackage string
	pattern        *regexp.Regexp
}

// compilePkgpattern compiles a package pattern such as `<fmt>` or `util/.*`
// into a pkgpattern.
//
// - `<pattern>` indicates std lib packages matching `pattern`
// - `pattern ` indicates non std lib packages matching `pattern`
// - `third_parties` is a wildcard to match any third parties (i.e. non std lib,
// non working package)
func compilePkgpattern(workingPackage, expr string) (*pkgpattern, error) {
	var p pkgpattern

	if expr == "third_parties" {
		p.thirdParties = true
		p.workingPackage = workingPackage
		return &p, nil
	}

	pattern := expr
	if strings.HasPrefix(expr, "<") && strings.HasSuffix(expr, ">") {
		pattern = expr[1 : len(expr)-1]
		p.goroot = true
	}

	var err error
	p.pattern, err = regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	return &p, nil
}

func (p *pkgpattern) match(pkg *pkg) bool {
	if p.goroot != pkg.goroot {
		return false
	}

	if p.thirdParties {
		return !strings.HasPrefix(pkg.name, p.workingPackage)
	}

	if !p.pattern.MatchString(pkg.name) {
		return false
	}

	return true
}

func (p *pkgpattern) String() string {
	if p.goroot {
		return fmt.Sprintf("<%s>", p.pattern)
	} else if p.thirdParties {
		return "third_parties"
	} else {
		return p.pattern.String()
	}
}

func parse(input []byte) (*defs, error) {
	// yaml parse
	var defs defs
	err := yaml.Unmarshal([]byte(input), &defs)
	if err != nil {
		return nil, err
	}

	// configuration
	if strings.HasSuffix(defs.Config.WorkingPackage, "/") {
		return nil, fmt.Errorf("must be package import path, was %s", defs.Config.WorkingPackage)
	}

	// process all rules
	for _, rule := range defs.Rules {
		var err error
		rule.packagePattern, err = regexp.Compile("^" + defs.Config.WorkingPackage + "/" + rule.Packages + "$")
		if err != nil {
			return nil, err
		}
		for _, expr := range rule.MayDepend {
			set, err := compilePkgpattern(defs.Config.WorkingPackage, expr)
			if err != nil {
				return nil, err
			}
			rule.mayDepends = append(rule.mayDepends, set)
		}
		rule.expectedStarToPackage = make(map[string]bool)
		rule.expectedPackageToPackage = make(map[string]map[string]bool)
		for _, expected := range rule.Expected {
			parts := strings.Split(expected, "->")
			if l := len(parts); l == 1 {
				rule.expectedStarToPackage[defs.Config.WorkingPackage+"/"+expected] = true
			} else if l == 2 {
				parent := defs.Config.WorkingPackage + "/" + strings.TrimSpace(parts[0])
				child := defs.Config.WorkingPackage + "/" + strings.TrimSpace(parts[1])
				if _, ok := rule.expectedPackageToPackage[parent]; !ok {
					rule.expectedPackageToPackage[parent] = make(map[string]bool)
				}
				rule.expectedPackageToPackage[parent][child] = true
			} else {
				return nil, fmt.Errorf("malformed expectation %s", expected)
			}
		}
		rule.actualPackagesProcessed = make(map[string]bool)
	}

	return &defs, nil
}

func main() {
	var configPath string
	if len(os.Args) == 2 {
		configPath = os.Args[1]
	} else {
		fmt.Println("usage: depper config.yaml")
		os.Exit(1)
	}

	bytes, err := ioutil.ReadFile(configPath)
	if err != nil {
		panic(err)
	}
	defs, err := parse(bytes)
	if err != nil {
		panic(err)
	}

	// Collect all packages.
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	pkgs, err := defs.collectPackages(cwd)
	if err != nil {
		panic(err)
	}

	// Run all packages against rules.
	for _, pkg := range pkgs {
		for _, rule := range defs.Rules {
			if rule.packagePattern.MatchString(pkg.name) {
				rule.process(pkgs, pkg)
			}
		}
	}

	// Missing packaged?
	for _, rule := range defs.Rules {
		rule.processMissingPackages()
	}

	// Print all violations.
	ok := true
	for _, rule := range defs.Rules {
		if len(rule.violations) != 0 {
			fmt.Println(rule.Name)
			for _, violation := range rule.violations {
				fmt.Println(violation)
				ok = false
			}
		}
	}

	// Status code.
	if !ok {
		os.Exit(1)
	}
	os.Exit(0)
}

func (rule *rule) process(pkgs map[string]*pkg, pkg *pkg) {
	var (
		bads            []string
		starActuals     = make(map[string]bool)
		specificActuals = make(map[string]bool)
	)

	// Process.
	rule.actualPackagesProcessed[pkg.name] = true

nextPkg:
	for _, depPkg := range pkg.dependsOn {
		for _, set := range rule.mayDepends {
			if set.match(depPkg) {
				continue nextPkg
			}
		}

		// Exception for whole rule?
		if rule.expectedStarToPackage[depPkg.name] {
			starActuals[depPkg.name] = true
			continue nextPkg
		}

		// Exception for specific dependency?
		if _, ok := rule.expectedPackageToPackage[pkg.name]; ok {
			if rule.expectedPackageToPackage[pkg.name][depPkg.name] {
				specificActuals[depPkg.name] = true
				continue nextPkg
			}
		}

		// Bad.
		bads = append(bads, depPkg.name)
	}

	// Handle violations.
	for _, bad := range bads {
		rule.violations = append(rule.violations, fmt.Sprintf("- disallowed %s -> %s", pkg, bad))
	}
	for expected, _ := range rule.expectedStarToPackage {
		if expected == pkg.name {
			continue
		}
		if !starActuals[expected] {
			rule.violations = append(rule.violations, fmt.Sprintf("- expected   %s -> %s", pkg, expected))
		}
	}
	for expected, _ := range rule.expectedPackageToPackage[pkg.name] {
		if expected == pkg.name {
			continue
		}
		if !specificActuals[expected] {
			rule.violations = append(rule.violations, fmt.Sprintf("- expected   %s -> %s", pkg, expected))
		}
	}
}

func (rule *rule) processMissingPackages() {
	for expected, _ := range rule.expectedPackageToPackage {
		if !rule.actualPackagesProcessed[expected] {
			rule.violations = append(rule.violations, fmt.Sprintf("- missing    %s", expected))
		}
	}
}

func isGoroot(goPkg *packages.Package) bool {
	return strings.HasPrefix(goPkg.GoFiles[0], runtime.GOROOT())
}

func (defs *defs) collectPackages(root string) (map[string]*pkg, error) {
	pkgs := make(map[string]*pkg)
	if err := defs._collectPackages(pkgs, root, ".", 0); err != nil {
		return nil, err
	}
	return pkgs, nil
}

func (defs *defs) _collectPackages(pkgs map[string]*pkg, root string, pkgName string, level int) error {
	if level++; level > 256 {
		return nil
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedFiles,
		Dir:  root,
	}

	goPkgs, err := packages.Load(cfg, pkgName)
	if err != nil {
		return fmt.Errorf("failed to import %s: %s", pkgName, err)
	}
	goPkg := goPkgs[0]
	if pkgName == "." {
		pkgName = goPkg.ID
	}

	pkg := pkg{
		name:      pkgName,
		goroot:    isGoroot(goPkg),
		dependsOn: make(map[string]*pkg),
	}
	pkgs[pkgName] = &pkg

	// Don't worry about dependencies for stdlib packages
	if pkg.goroot {
		return nil
	}

	// Don't worry about dependencies for non working packages
	if !strings.HasPrefix(pkgName, defs.Config.WorkingPackage) {
		return nil
	}

	for _, imp := range getImports(goPkg) {
		if _, ok := pkgs[imp]; !ok {
			if err := defs._collectPackages(pkgs, root, imp, level); err != nil {
				return err
			}
		}
		pkg.dependsOn[imp] = pkgs[imp]
	}

	return nil
}

func getImports(goPkg *packages.Package) []string {
	var imports []string
	found := make(map[string]bool)
	for key := range goPkg.Imports {
		if key == goPkg.ID {
			// Don't draw a self-reference when foo_test depends on foo.
			continue
		}
		if found[key] {
			continue
		}
		found[key] = true
		imports = append(imports, key)
	}
	return imports
}
