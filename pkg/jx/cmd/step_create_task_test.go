package cmd_test

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	gits_test "github.com/jenkins-x/jx/pkg/gits/mocks"
	helm_test "github.com/jenkins-x/jx/pkg/helm/mocks"
	"github.com/jenkins-x/jx/pkg/kube"
	"github.com/knative/pkg/kmp"
	uuid "github.com/satori/go.uuid"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/jenkins-x/jx/pkg/config"
	"github.com/jenkins-x/jx/pkg/gits"
	"github.com/jenkins-x/jx/pkg/jenkinsfile"
	"github.com/jenkins-x/jx/pkg/jx/cmd"
	"github.com/jenkins-x/jx/pkg/jx/cmd/opts"
	"github.com/jenkins-x/jx/pkg/tekton/tekton_helpers_test"
	"github.com/jenkins-x/jx/pkg/tests"
	"github.com/stretchr/testify/assert"
	pipelineapi "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateTektonCRDs(t *testing.T) {
	tests.SkipForWindows(t, "go-expect does not work on windows")
	t.Parallel()

	testVersionsDir := path.Join("test_data", "cmmon_versions")
	testData := path.Join("test_data", "step_create_task")
	_, err := os.Stat(testData)
	assert.NoError(t, err)

	packsDir := path.Join(testData, "packs")
	_, err = os.Stat(packsDir)
	assert.NoError(t, err)

	resolver := func(importFile *jenkinsfile.ImportFile) (string, error) {
		dirPath := []string{packsDir, "import_dir", importFile.Import}
		// lets handle cross platform paths in `importFile.File`
		path := append(dirPath, strings.Split(importFile.File, "/")...)
		return filepath.Join(path...), nil
	}

	cases := []struct {
		name                string
		language            string
		repoName            string
		organization        string
		branch              string
		kind                string
		expectingError      bool
		expectedActivityKey *kube.PromoteStepActivityKey
	}{
		{
			name:         "js_build_pack",
			language:     "javascript",
			repoName:     "js-test-repo",
			organization: "abayer",
			branch:       "build-pack",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-js-test-repo-build-pack-1",
					Pipeline: "abayer/js-test-repo/build-pack",
					Build:    "1",
				},
			},
		},
		{
			name:         "maven_build_pack",
			language:     "maven",
			repoName:     "jx-demo-qs",
			organization: "abayer",
			branch:       "master",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-jx-demo-qs-master-1",
					Pipeline: "abayer/jx-demo-qs/master",
					Build:    "1",
				},
			},
		},
		{
			name:         "from_yaml",
			language:     "none",
			repoName:     "js-test-repo",
			organization: "abayer",
			branch:       "really-long",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-js-test-repo-really-long-1",
					Pipeline: "abayer/js-test-repo/really-long",
					Build:    "1",
				},
			},
		},
		{
			name:           "no_pipeline_config",
			language:       "none",
			repoName:       "anything",
			organization:   "anything",
			branch:         "anything",
			kind:           "release",
			expectingError: true,
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "anything-anything-anything-1",
					Pipeline: "anything/anything/anything",
					Build:    "1",
				},
			},
		},
		{
			name:         "per_step_container_build_pack",
			language:     "apps",
			repoName:     "golang-qs-test",
			organization: "abayer",
			branch:       "master",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-golang-qs-test-master-1",
					Pipeline: "abayer/golang-qs-test/master",
					Build:    "1",
				},
			},
		},
		{
			name:         "kaniko_entrypoint",
			language:     "none",
			repoName:     "jx",
			organization: "jenkins-x",
			branch:       "fix-kaniko-special-casing",
			kind:         "pullrequest",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "jenkins-x-jx-fix-kaniko-special-casing-1",
					Pipeline: "jenkins-x/jx/fix-kaniko-special-casing",
					Build:    "1",
				},
			},
		},
		{
			name:         "set-agent-container-with-agentless-build-pack",
			language:     "no-default-agent",
			repoName:     "js-test-repo",
			organization: "abayer",
			branch:       "no-default-agent",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-js-test-repo-no-default-agent-1",
					Pipeline: "abayer/js-test-repo/no-default-agent",
					Build:    "1",
				},
			},
		},
		{
			name:         "override-agent-container-with-build-pack",
			language:     "override-default-agent",
			repoName:     "js-test-repo",
			organization: "abayer",
			branch:       "override-default-agent",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-js-test-repo-override-default-agent-1",
					Pipeline: "abayer/js-test-repo/override-default-agent",
					Build:    "1",
				},
			},
		},
		{
			name:         "override-steps",
			language:     "maven",
			repoName:     "jx-demo-qs",
			organization: "abayer",
			branch:       "master",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-jx-demo-qs-master-1",
					Pipeline: "abayer/jx-demo-qs/master",
					Build:    "1",
				},
			},
		},
		{
			name:         "override_block_step",
			language:     "apps",
			repoName:     "golang-qs-test",
			organization: "abayer",
			branch:       "master",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-golang-qs-test-master-1",
					Pipeline: "abayer/golang-qs-test/master",
					Build:    "1",
				},
			},
		},
		{
			name:         "loop-in-buildpack-syntax",
			language:     "maven",
			repoName:     "jx-demo-qs",
			organization: "abayer",
			branch:       "master",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-jx-demo-qs-master-1",
					Pipeline: "abayer/jx-demo-qs/master",
					Build:    "1",
				},
			},
		},
		{
			name:         "containeroptions-on-pipelineconfig",
			language:     "maven",
			repoName:     "jx-demo-qs",
			organization: "abayer",
			branch:       "master",
			kind:         "release",
			expectedActivityKey: &kube.PromoteStepActivityKey{
				PipelineActivityKey: kube.PipelineActivityKey{
					Name:     "abayer-jx-demo-qs-master-1",
					Pipeline: "abayer/jx-demo-qs/master",
					Build:    "1",
				},
			},
		},
	}

	k8sObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kube.ConfigMapJenkinsDockerRegistry,
				Namespace: "jx",
			},
			Data: map[string]string{
				"docker.registry": "1.2.3.4:5000",
			},
		},
	}
	jxObjects := []runtime.Object{}
	repoOwnerUUID, err := uuid.NewV4()
	assert.NoError(t, err)
	repoOwner := repoOwnerUUID.String()
	repoNameUUID, err := uuid.NewV4()
	assert.NoError(t, err)
	repoName := repoNameUUID.String()
	fakeRepo := gits.NewFakeRepository(repoOwner, repoName)
	fakeGitProvider := gits.NewFakeProvider(fakeRepo)

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {

			caseDir := path.Join(testData, tt.name)
			_, err = os.Stat(caseDir)
			assert.NoError(t, err)

			projectConfig, projectConfigFile, err := config.LoadProjectConfig(caseDir)
			if err != nil {
				t.Fatalf("Error loading %s/jenkins-x.yml: %s", caseDir, err)
			}

			createTask := &cmd.StepCreateTaskOptions{
				Pack:             tt.language,
				NoReleasePrepare: false,
				NoApply:          true,
				SourceName:       "source",
				PodTemplates:     assertLoadPodTemplates(t),
				GitInfo: &gits.GitRepository{
					Host:         "github.com",
					Name:         tt.repoName,
					Organisation: tt.organization,
				},
				Branch:       tt.branch,
				PipelineKind: tt.kind,
				NoKaniko:     true,
				Trigger:      string(pipelineapi.PipelineTriggerTypeManual),
				StepOptions: cmd.StepOptions{
					CommonOptions: &opts.CommonOptions{
						ServiceAccount: "tekton-bot",
					},
				},
				BuildNumber: "1",
				VersionResolver: &opts.VersionResolver{
					VersionsDir: testVersionsDir,
				},
				DefaultImage: "maven",
			}
			cmd.ConfigureTestOptionsWithResources(createTask.CommonOptions, k8sObjects, jxObjects, gits_test.NewMockGitter(), fakeGitProvider, helm_test.NewMockHelmer(), nil)

			pipeline, tasks, resources, run, structure, err := createTask.GenerateTektonCRDs(packsDir, projectConfig, projectConfigFile, resolver, "jx")
			if tt.expectingError {
				if err == nil {
					t.Fatalf("Expected an error generating CRDs")
				}
			} else {
				if err != nil {
					t.Fatalf("Error generating CRDs: %s", err)
				}

				taskList := &pipelineapi.TaskList{}
				for _, task := range tasks {
					taskList.Items = append(taskList.Items, *task)
				}

				resourceList := &pipelineapi.PipelineResourceList{}
				for _, resource := range resources {
					resourceList.Items = append(resourceList.Items, *resource)
				}

				if d := cmp.Diff(tekton_helpers_test.AssertLoadPipeline(t, caseDir), pipeline); d != "" {
					p, _ := yaml.Marshal(pipeline)
					println(string(p))
					t.Errorf("Generated Pipeline did not match expected: \n%s", d)
				}
				if d, _ := kmp.SafeDiff(tekton_helpers_test.AssertLoadTasks(t, caseDir), taskList, cmpopts.IgnoreFields(corev1.ResourceRequirements{}, "Requests")); d != "" {
					p, _ := yaml.Marshal(taskList)
					println("TaskList error")
					println(string(p))
					t.Errorf("Generated Tasks did not match expected: \n%s", d)
				}
				if d := cmp.Diff(tekton_helpers_test.AssertLoadPipelineResources(t, caseDir), resourceList); d != "" {
					p, _ := yaml.Marshal(resourceList)
					println(string(p))
					t.Errorf("Generated PipelineResources did not match expected: %s", d)
				}
				if d := cmp.Diff(tekton_helpers_test.AssertLoadPipelineRun(t, caseDir), run); d != "" {
					p, _ := yaml.Marshal(run)
					println(string(p))
					t.Errorf("Generated PipelineRun did not match expected: %s", d)
				}
				if d := cmp.Diff(tekton_helpers_test.AssertLoadPipelineStructure(t, caseDir), structure); d != "" {
					p, _ := yaml.Marshal(structure)
					println(string(p))
					t.Errorf("Generated PipelineStructure did not match expected: %s", d)
				}
			}

			pa := createTask.GeneratePipelineActivity("jx")

			tt.expectedActivityKey.GitInfo = createTask.GitInfo
			if d := cmp.Diff(pa, tt.expectedActivityKey); d != "" {
				t.Errorf("not match expected: %s", d)
			}
		})
	}
}

func assertLoadPodTemplates(t *testing.T) map[string]*corev1.Pod {
	fileName := filepath.Join("test_data", "step_create_task", "podTemplates.yml")
	if tests.AssertFileExists(t, fileName) {
		configMap := &corev1.ConfigMap{}
		data, err := ioutil.ReadFile(fileName)
		if assert.NoError(t, err, "Failed to load file %s", fileName) {
			err = yaml.Unmarshal(data, configMap)
			if assert.NoError(t, err, "Failed to unmarshall YAML file %s", fileName) {
				podTemplates := make(map[string]*corev1.Pod)
				for k, v := range configMap.Data {
					pod := &corev1.Pod{}
					if v != "" {
						err := yaml.Unmarshal([]byte(v), pod)
						if assert.NoError(t, err, "Failed to parse pod template") {
							podTemplates[k] = pod
						}
					}
				}
				return podTemplates
			}
		}
	}
	return nil
}
