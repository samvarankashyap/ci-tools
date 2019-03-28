package diffs

import (
	"reflect"
	"testing"

	"github.com/getlantern/deepcopy"
	"github.com/sirupsen/logrus"

	v1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/apimachinery/pkg/util/sets"

	pjapi "k8s.io/test-infra/prow/apis/prowjobs/v1"
	prowconfig "k8s.io/test-infra/prow/config"

	templateapi "github.com/openshift/api/template/v1"

	cioperatorapi "github.com/openshift/ci-operator/pkg/api"

	"github.com/openshift/ci-operator-prowgen/pkg/config"
)

func TestGetChangedCiopConfigs(t *testing.T) {
	baseCiopConfig := cioperatorapi.ReleaseBuildConfiguration{
		InputConfiguration: cioperatorapi.InputConfiguration{
			ReleaseTagConfiguration: &cioperatorapi.ReleaseTagConfiguration{
				Cluster:   "kluster",
				Namespace: "namespace",
				Name:      "name",
			},
		},
		Tests: []cioperatorapi.TestStepConfiguration{
			{
				As:       "unit",
				Commands: "make unit",
			},
			{
				As:       "e2e",
				Commands: "make e2e",
			},
			{
				As:       "verify",
				Commands: "make verify",
			},
		},
	}

	testCases := []struct {
		name                 string
		configGenerator      func() (before, after config.CompoundCiopConfig)
		expected             func() config.CompoundCiopConfig
		expectedAffectedJobs map[string]sets.String
	}{{
		name: "no changes",
		configGenerator: func() (config.CompoundCiopConfig, config.CompoundCiopConfig) {
			before := config.CompoundCiopConfig{"org-repo-branch.yaml": &baseCiopConfig}
			after := config.CompoundCiopConfig{"org-repo-branch.yaml": &baseCiopConfig}
			return before, after
		},
		expected:             func() config.CompoundCiopConfig { return config.CompoundCiopConfig{} },
		expectedAffectedJobs: map[string]sets.String{},
	}, {
		name: "new config",
		configGenerator: func() (config.CompoundCiopConfig, config.CompoundCiopConfig) {
			before := config.CompoundCiopConfig{"org-repo-branch.yaml": &baseCiopConfig}
			after := config.CompoundCiopConfig{
				"org-repo-branch.yaml":         &baseCiopConfig,
				"org-repo-another-branch.yaml": &baseCiopConfig,
			}
			return before, after
		},
		expected: func() config.CompoundCiopConfig {
			return config.CompoundCiopConfig{"org-repo-another-branch.yaml": &baseCiopConfig}
		},
		expectedAffectedJobs: map[string]sets.String{},
	}, {
		name: "changed config",
		configGenerator: func() (config.CompoundCiopConfig, config.CompoundCiopConfig) {
			before := config.CompoundCiopConfig{"org-repo-branch.yaml": &baseCiopConfig}
			afterConfig := cioperatorapi.ReleaseBuildConfiguration{}
			deepcopy.Copy(&afterConfig, baseCiopConfig)
			afterConfig.InputConfiguration.ReleaseTagConfiguration.Name = "another-name"
			after := config.CompoundCiopConfig{"org-repo-branch.yaml": &afterConfig}
			return before, after
		},
		expected: func() config.CompoundCiopConfig {
			expected := cioperatorapi.ReleaseBuildConfiguration{}
			deepcopy.Copy(&expected, baseCiopConfig)
			expected.InputConfiguration.ReleaseTagConfiguration.Name = "another-name"
			return config.CompoundCiopConfig{"org-repo-branch.yaml": &expected}
		},
		expectedAffectedJobs: map[string]sets.String{},
	},
		{
			name: "changed tests",
			configGenerator: func() (config.CompoundCiopConfig, config.CompoundCiopConfig) {
				before := config.CompoundCiopConfig{"org-repo-branch.yaml": &baseCiopConfig}
				afterConfig := cioperatorapi.ReleaseBuildConfiguration{}
				deepcopy.Copy(&afterConfig, baseCiopConfig)
				afterConfig.Tests[0].Commands = "changed commands"
				after := config.CompoundCiopConfig{"org-repo-branch.yaml": &afterConfig}
				return before, after
			},
			expected: func() config.CompoundCiopConfig {
				expected := cioperatorapi.ReleaseBuildConfiguration{}
				deepcopy.Copy(&expected, baseCiopConfig)
				expected.Tests[0].Commands = "changed commands"
				return config.CompoundCiopConfig{"org-repo-branch.yaml": &expected}
			},
			expectedAffectedJobs: map[string]sets.String{"org-repo-branch.yaml": {"unit": sets.Empty{}}},
		},
		{
			name: "changed multiple tests",
			configGenerator: func() (config.CompoundCiopConfig, config.CompoundCiopConfig) {
				before := config.CompoundCiopConfig{"org-repo-branch.yaml": &baseCiopConfig}
				afterConfig := cioperatorapi.ReleaseBuildConfiguration{}
				deepcopy.Copy(&afterConfig, baseCiopConfig)
				afterConfig.Tests[0].Commands = "changed commands"
				afterConfig.Tests[1].Commands = "changed commands"
				after := config.CompoundCiopConfig{"org-repo-branch.yaml": &afterConfig}
				return before, after
			},
			expected: func() config.CompoundCiopConfig {
				expected := cioperatorapi.ReleaseBuildConfiguration{}
				deepcopy.Copy(&expected, baseCiopConfig)
				expected.Tests[0].Commands = "changed commands"
				expected.Tests[1].Commands = "changed commands"
				return config.CompoundCiopConfig{"org-repo-branch.yaml": &expected}
			},
			expectedAffectedJobs: map[string]sets.String{
				"org-repo-branch.yaml": {
					"unit": sets.Empty{},
					"e2e":  sets.Empty{},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			before, after := tc.configGenerator()
			actual, affectedJobs := GetChangedCiopConfigs(before, after, logrus.NewEntry(logrus.New()))
			expected := tc.expected()

			if !reflect.DeepEqual(expected, actual) {
				t.Errorf("Detected changed ci-operator config changes differ from expected:\n%s", diff.ObjectReflectDiff(expected, actual))
			}

			if !reflect.DeepEqual(tc.expectedAffectedJobs, affectedJobs) {
				t.Errorf("Affected jobs differ from expected:\n%s", diff.ObjectReflectDiff(tc.expectedAffectedJobs, affectedJobs))
			}
		})
	}
}

func TestGetChangedPresubmits(t *testing.T) {
	basePresubmit := []prowconfig.Presubmit{
		{
			JobBase: prowconfig.JobBase{
				Agent: "kubernetes",
				Name:  "test-base-presubmit",
				Spec: &v1.PodSpec{
					Containers: []v1.Container{{
						Command: []string{"ci-operator"},
						Args:    []string{"--artifact-dir=$(ARTIFACTS)", "--target=images"},
					}},
				},
			},
			Context:  "test-base-presubmit",
			Brancher: prowconfig.Brancher{Branches: []string{"^master$"}},
		},
	}

	testCases := []struct {
		name            string
		configGenerator func() (before, after *prowconfig.Config)
		expected        config.Presubmits
	}{
		{
			name: "no differences mean nothing is identified as a diff",
			configGenerator: func() (*prowconfig.Config, *prowconfig.Config) {
				return makeConfig(basePresubmit), makeConfig(basePresubmit)
			},
			expected: config.Presubmits{},
		},
		{
			name: "new job added",
			configGenerator: func() (*prowconfig.Config, *prowconfig.Config) {
				var p []prowconfig.Presubmit
				var pNew prowconfig.Presubmit
				deepcopy.Copy(&p, basePresubmit)

				pNew = p[0]
				pNew.Name = "test-base-presubmit-new"
				p = append(p, pNew)

				return makeConfig(basePresubmit), makeConfig(p)

			},
			expected: config.Presubmits{
				"org/repo": func() []prowconfig.Presubmit {
					var p []prowconfig.Presubmit
					var pNew prowconfig.Presubmit
					deepcopy.Copy(&p, basePresubmit)
					pNew = p[0]
					pNew.Name = "test-base-presubmit-new"

					return []prowconfig.Presubmit{pNew}
				}(),
			},
		},
		{
			name: "different agent is identified as a diff (from jenkins to kubernetes)",
			configGenerator: func() (*prowconfig.Config, *prowconfig.Config) {
				var p []prowconfig.Presubmit
				deepcopy.Copy(&p, basePresubmit)
				p[0].Agent = "jenkins"
				return makeConfig(p), makeConfig(basePresubmit)

			},
			expected: config.Presubmits{
				"org/repo": basePresubmit,
			},
		},
		{
			name: "different spec is identified as a diff - single change",
			configGenerator: func() (*prowconfig.Config, *prowconfig.Config) {
				var p []prowconfig.Presubmit
				deepcopy.Copy(&p, basePresubmit)
				p[0].Spec.Containers[0].Command = []string{"test-command"}
				return makeConfig(basePresubmit), makeConfig(p)

			},
			expected: config.Presubmits{
				"org/repo": func() []prowconfig.Presubmit {
					var p []prowconfig.Presubmit
					deepcopy.Copy(&p, basePresubmit)
					p[0].Spec.Containers[0].Command = []string{"test-command"}
					return p
				}(),
			},
		},
		{
			name: "different spec is identified as a diff - massive changes",
			configGenerator: func() (*prowconfig.Config, *prowconfig.Config) {
				var p []prowconfig.Presubmit
				deepcopy.Copy(&p, basePresubmit)
				p[0].Spec.Containers[0].Command = []string{"test-command"}
				p[0].Spec.Containers[0].Args = []string{"testarg", "testarg", "testarg"}
				p[0].Spec.Volumes = []v1.Volume{{
					Name: "test-volume",
					VolumeSource: v1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{},
					}},
				}
				return makeConfig(basePresubmit), makeConfig(p)
			},
			expected: config.Presubmits{
				"org/repo": func() []prowconfig.Presubmit {
					var p []prowconfig.Presubmit
					deepcopy.Copy(&p, basePresubmit)
					p[0].Spec.Containers[0].Command = []string{"test-command"}
					p[0].Spec.Containers[0].Args = []string{"testarg", "testarg", "testarg"}
					p[0].Spec.Volumes = []v1.Volume{{
						Name: "test-volume",
						VolumeSource: v1.VolumeSource{
							EmptyDir: &v1.EmptyDirVolumeSource{},
						}},
					}
					return p
				}()},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			before, after := testCase.configGenerator()
			p := GetChangedPresubmits(before, after, logrus.NewEntry(logrus.New()))
			if !equality.Semantic.DeepEqual(p, testCase.expected) {
				t.Fatalf("Name:%s\nExpected %#v\nFound:%#v\n", testCase.name, testCase.expected["org/repo"], p["org/repo"])
			}
		})
	}
}

func makeConfig(p []prowconfig.Presubmit) *prowconfig.Config {
	return &prowconfig.Config{
		JobConfig: prowconfig.JobConfig{
			Presubmits: config.Presubmits{"org/repo": p},
		},
	}
}

func TestGetChangedTemplates(t *testing.T) {
	baseParameters := []templateapi.Parameter{
		{
			Name:     "JOB_NAME_SAFE",
			Required: true,
		},
		{
			Name:     "JOB_NAME_HASH",
			Required: true,
		},
		{
			Name:     "NAMESPACE",
			Required: true,
		},
		{
			Name:     "IMAGE_FORMAT",
			Required: true,
		},
		{
			Name:     "CLUSTER_TYPE",
			Required: true,
		},
		{
			Name:     "TEST_COMMAND",
			Required: true,
		},
	}

	baseObjects := []runtime.RawExtension{
		{
			Object: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:    "setup",
							Image:   "setup-image",
							Command: []string{"bash-script"},
						},
						{
							Name:    "test",
							Image:   "test-image",
							Command: []string{"bash-script"},
						},
						{
							Image:   "teardown-image",
							Name:    "teardown",
							Command: []string{"bash-script"},
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "test-volume",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{}},
						},
					},
				},
			},
		},
	}
	testCases := []struct {
		name         string
		getTemplates func() (map[string]*templateapi.Template, map[string]*templateapi.Template)
		expected     config.CiTemplates
	}{
		{
			name: "no changes",
			getTemplates: func() (map[string]*templateapi.Template, map[string]*templateapi.Template) {
				templates := makeBaseTemplates(baseParameters, baseObjects)
				return templates, templates
			},
			expected: config.CiTemplates{},
		},
		{
			name: "add new template",
			getTemplates: func() (map[string]*templateapi.Template, map[string]*templateapi.Template) {
				prTemplates := makeBaseTemplates(baseParameters, baseObjects)

				prTemplates["templateNEW"] = &templateapi.Template{
					ObjectMeta: metav1.ObjectMeta{
						Name: "templateNEW",
						Annotations: map[string]string{
							"description": "description",
						},
					},
					Parameters: baseParameters,
					Objects:    baseObjects,
				}
				return makeBaseTemplates(baseParameters, baseObjects), prTemplates
			},
			expected: map[string]*templateapi.Template{
				"templateNEW": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "templateNEW",
						Annotations: map[string]string{
							"description": "description",
						},
					},
					Parameters: baseParameters,
					Objects:    baseObjects,
				},
			},
		},
		{
			name: "change existing template",
			getTemplates: func() (map[string]*templateapi.Template, map[string]*templateapi.Template) {
				prTemplates := makeBaseTemplates(baseParameters, baseObjects)

				prTemplates["template1"].Parameters = append(prTemplates["template1"].Parameters,
					templateapi.Parameter{
						Name:  "NEW_PARAM",
						Value: "TEST_VALUE",
					})
				return makeBaseTemplates(baseParameters, baseObjects), prTemplates
			},
			expected: config.CiTemplates{
				"template1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "template1",
						Annotations: map[string]string{
							"description": "description",
						},
					},
					Parameters: append(baseParameters, templateapi.Parameter{
						Name:  "NEW_PARAM",
						Value: "TEST_VALUE",
					}),
					Objects: baseObjects,
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			before, after := testCase.getTemplates()
			changedTemplates := GetChangedTemplates(before, after, logrus.WithField("testcase", testCase.name))
			if !equality.Semantic.DeepEqual(changedTemplates, testCase.expected) {
				t.Fatalf("Name:%s\nExpected %#v\nFound:%#v\n", testCase.name, testCase.expected, changedTemplates)
			}
		})
	}
}

func makeBaseTemplates(baseParameters []templateapi.Parameter, baseObjects []runtime.RawExtension) map[string]*templateapi.Template {
	return map[string]*templateapi.Template{
		"template1": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "template1",
				Annotations: map[string]string{
					"description": "description",
				},
			},
			Parameters: baseParameters,
			Objects:    baseObjects,
		},
		"template2": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "template2",
				Annotations: map[string]string{
					"description": "description",
				},
			},
			Parameters: baseParameters,
			Objects:    baseObjects,
		},
	}
}

func TestGetPresubmitsForCiopConfigs(t *testing.T) {
	baseCiopConfig := config.Info{
		Org:      "org",
		Repo:     "repo",
		Branch:   "branch",
		Filename: "org-repo-branch.yaml",
	}

	basePresubmitWithCiop := prowconfig.Presubmit{
		Brancher: prowconfig.Brancher{
			Branches: []string{baseCiopConfig.Branch},
		},
		JobBase: prowconfig.JobBase{
			Agent: string(pjapi.KubernetesAgent),
			Spec: &v1.PodSpec{
				Containers: []v1.Container{{
					Env: []v1.EnvVar{{
						ValueFrom: &v1.EnvVarSource{
							ConfigMapKeyRef: &v1.ConfigMapKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: baseCiopConfig.ConfigMapName(),
								},
							},
						},
					}},
				}},
			},
		},
	}

	affectedJobs := map[string]sets.String{
		"org-repo-branch.yaml": {
			"testjob": sets.Empty{},
		},
	}

	testCases := []struct {
		description string
		prow        *prowconfig.Config
		ciop        config.CompoundCiopConfig
		expected    config.Presubmits
	}{{
		description: "return a presubmit using one of the input ciop configs",
		prow: &prowconfig.Config{
			JobConfig: prowconfig.JobConfig{
				Presubmits: map[string][]prowconfig.Presubmit{
					"org/repo": {
						func() prowconfig.Presubmit {
							ret := prowconfig.Presubmit{}
							deepcopy.Copy(&ret, &basePresubmitWithCiop)
							ret.Name = "org-repo-branch-testjob"
							ret.Spec.Containers[0].Env[0].ValueFrom.ConfigMapKeyRef.Key = baseCiopConfig.Filename
							return ret
						}(),
					}},
			},
		},
		ciop: config.CompoundCiopConfig{baseCiopConfig.Filename: &cioperatorapi.ReleaseBuildConfiguration{}},
		expected: config.Presubmits{"org/repo": {
			func() prowconfig.Presubmit {
				ret := prowconfig.Presubmit{}
				deepcopy.Copy(&ret, &basePresubmitWithCiop)
				ret.Name = "org-repo-branch-testjob"
				ret.Spec.Containers[0].Env[0].ValueFrom.ConfigMapKeyRef.Key = baseCiopConfig.Filename
				return ret
			}(),
		}},
	}, {
		description: "do not return a presubmit using a ciop config not present in input",
		prow: &prowconfig.Config{
			JobConfig: prowconfig.JobConfig{
				Presubmits: map[string][]prowconfig.Presubmit{
					"org/repo": {
						func() prowconfig.Presubmit {
							ret := prowconfig.Presubmit{}
							deepcopy.Copy(&ret, &basePresubmitWithCiop)
							ret.Name = "org-repo-branch-testjob"
							ret.Spec.Containers[0].Env[0].ValueFrom.ConfigMapKeyRef.Key = baseCiopConfig.Filename
							return ret
						}(),
					}},
			},
		},
		ciop:     config.CompoundCiopConfig{},
		expected: config.Presubmits{},
	}, {
		description: "handle jenkins presubmits",
		prow: &prowconfig.Config{
			JobConfig: prowconfig.JobConfig{
				Presubmits: map[string][]prowconfig.Presubmit{
					"org/repo": {
						func() prowconfig.Presubmit {
							ret := prowconfig.Presubmit{}
							deepcopy.Copy(&ret, &basePresubmitWithCiop)
							ret.Name = "org-repo-branch-testjob"
							ret.Agent = string(pjapi.JenkinsAgent)
							ret.Spec.Containers[0].Env = []v1.EnvVar{}
							return ret
						}(),
					}},
			},
		},
		ciop:     config.CompoundCiopConfig{},
		expected: config.Presubmits{},
	},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			presubmits := GetPresubmitsForCiopConfigs(tc.prow, tc.ciop, logrus.NewEntry(logrus.New()), affectedJobs)

			if !reflect.DeepEqual(tc.expected, presubmits) {
				t.Errorf("Returned presubmits differ from expected:\n%s", diff.ObjectDiff(tc.expected, presubmits))
			}
		})
	}
}

func TestGetPresubmitsForClusterProfiles(t *testing.T) {
	makePresubmit := func(name string, agent pjapi.ProwJobAgent, profiles []string) prowconfig.Presubmit {
		ret := prowconfig.Presubmit{
			JobBase: prowconfig.JobBase{
				Name:  name,
				Agent: string(agent),
				Spec:  &v1.PodSpec{}},
		}
		for _, p := range profiles {
			ret.Spec.Volumes = append(ret.Spec.Volumes, v1.Volume{
				Name: "cluster-profile",
				VolumeSource: v1.VolumeSource{
					Projected: &v1.ProjectedVolumeSource{
						Sources: []v1.VolumeProjection{{
							ConfigMap: &v1.ConfigMapProjection{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "cluster-profile-" + p,
								},
							},
						}},
					},
				},
			})
		}
		return ret
	}
	logger := logrus.NewEntry(logrus.New())
	for _, tc := range []struct {
		id       string
		cfg      *prowconfig.Config
		profiles []config.ClusterProfile
		expected []string
	}{{
		id:       "empty",
		cfg:      &prowconfig.Config{},
		profiles: []config.ClusterProfile{{Name: "test-profile"}},
	}, {
		id: "not a kubernetes job",
		cfg: &prowconfig.Config{
			JobConfig: prowconfig.JobConfig{
				Presubmits: map[string][]prowconfig.Presubmit{
					"org/repo": {
						makePresubmit("not-a-kubernetes-job", pjapi.JenkinsAgent, []string{}),
					},
				},
			},
		},
		profiles: []config.ClusterProfile{{Name: "test-profile"}},
	}, {
		id: "job doesn't use cluster profiles",
		cfg: &prowconfig.Config{
			JobConfig: prowconfig.JobConfig{
				Presubmits: map[string][]prowconfig.Presubmit{
					"org/repo": {
						makePresubmit("no-cluster-profile", pjapi.KubernetesAgent, []string{}),
					},
				},
			},
		},
		profiles: []config.ClusterProfile{{Name: "test-profile"}},
	}, {
		id: "job doesn't use the cluster profile",
		cfg: &prowconfig.Config{
			JobConfig: prowconfig.JobConfig{
				Presubmits: map[string][]prowconfig.Presubmit{
					"org/repo": {
						makePresubmit("doesnt-use-cluster-profile", pjapi.KubernetesAgent, []string{"another-profile"}),
					},
				},
			},
		},
		profiles: []config.ClusterProfile{{Name: "test-profile"}},
	}, {
		id: "multiple jobs, one uses cluster the profile",
		cfg: &prowconfig.Config{
			JobConfig: prowconfig.JobConfig{
				Presubmits: map[string][]prowconfig.Presubmit{
					"org/repo": {
						makePresubmit("no-cluster-profile", pjapi.KubernetesAgent, []string{}),
					},
					"some/other-repo": {
						makePresubmit("uses-cluster-profile", pjapi.KubernetesAgent, []string{"test-profile"}),
						makePresubmit("uses-another-profile", pjapi.KubernetesAgent, []string{"another-profile"}),
					},
				},
			},
		},
		profiles: []config.ClusterProfile{{Name: "test-profile"}},
		expected: []string{"uses-cluster-profile"},
	}} {
		t.Run(tc.id, func(t *testing.T) {
			ret := GetPresubmitsForClusterProfiles(tc.cfg, tc.profiles, logger)
			var names []string
			for _, jobs := range ret {
				for _, j := range jobs {
					names = append(names, j.Name)
				}
			}
			if !reflect.DeepEqual(names, tc.expected) {
				t.Fatalf("want %s, got %s", tc.expected, names)
			}
		})
	}
}
