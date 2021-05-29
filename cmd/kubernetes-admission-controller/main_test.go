package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	anchore "github.com/anchore/kubernetes-admission-controller/pkg/anchore/client"
	"github.com/antihax/optional"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestConfigUpdate(t *testing.T) {
	//t.Skip("Disabled since it modifies local fs state")

	var tmp ControllerConfiguration
	configFileName := filepath.Join("testdata", "test_conf_init.json")

	src, ferr := os.Open(configFileName)
	if ferr != nil {
		t.Fatal(ferr, "Cannot find input config file")
	} else {
		defer src.Close()
	}

	tmpFileName := filepath.Join("testdata", "tmp_test_conf.json")
	dest, ferr2 := os.Create(tmpFileName)
	if ferr2 != nil {
		t.Fatal(ferr2, "Cannot open new tmp file")
	} else {
		defer dest.Close()
		defer os.Remove(tmpFileName)
	}

	_, ferr = io.Copy(dest, src)
	if ferr != nil {
		t.Fatal(ferr, "Could not create a copy of the config file for testing")
	}

	t.Log("Reading initial config")
	v := viper.New()
	v.SetConfigFile(tmpFileName)
	err := v.ReadInConfig()
	if err != nil {
		t.Fatal(err)
	}

	if v.Unmarshal(&tmp) != nil {
		t.Fatal(err)
	} else {
		cfg, _ := json.Marshal(tmp)
		t.Log("Got initial update test config: ", string(cfg))
	}

	v.OnConfigChange(func(in fsnotify.Event) {
		t.Log("Detected update and reloading!")
		if err = v.ReadInConfig(); err != nil {
			t.Fatal(err)
		}

		if err = v.Unmarshal(&tmp); err != nil {
			t.Fatal(err)
		}
	})

	t.Log("Watching the config")
	v.WatchConfig()

	var enabled bool

	t.Log("Starting the update cycler")
	for counter := 0; counter < 10; counter++ {
		t.Log("Waiting ", counter, " out of 10")
		time.Sleep(time.Duration(1 * time.Second))

		t.Log("Current config enabled flag: ", tmp.Validator.Enabled)
		enabled = tmp.Validator.Enabled

		if counter%2 != 0 {
			// Update the file, should cause a reload
			tmp2 := tmp
			tmp2.Validator.Enabled = !enabled
			tmpBytes, err := json.Marshal(tmp2)
			if err != nil {
				t.Fatal(err)

			}

			if len(tmpBytes) <= 0 {
				t.Fatal("No bytes found from marshalled struct")
			} else {
				t.Log("Updated config to write: ", string(tmpBytes))
			}

			t.Log("Writing updated config")
			fd, err := os.Create(tmpFileName)
			if err != nil {
				t.Fatal(err)

			}

			_, err = fd.Write(tmpBytes)
			if err != nil {
				t.Fatal(err)

			}

			if err = fd.Close(); err != nil {
				t.Fatal(err)

			}
		}
	}

	t.Log("Sleeping 1s to let things flush and close cleanly")
	time.Sleep(1 * time.Second)

	t.Log("Complete")
}

func TestConfig(t *testing.T) {
	v := viper.New()
	f, _ := os.Getwd()
	t.Log(f)
	configPath := filepath.Join("testdata", "test_conf.json")
	v.SetConfigFile(configPath)
	err := v.ReadInConfig()
	if err != nil {
		t.Fatal(err)

	}
	t.Log("Cfg State: ", v)
	var tmp ControllerConfiguration
	err = v.Unmarshal(&tmp)
	if err != nil {
		t.Fatal(err)

	} else {
		cfg, _ := json.Marshal(tmp)
		t.Log("Got config: ", string(cfg))
	}

	var tmp2 AnchoreAuthConfig
	v = viper.New()
	configPath2 := filepath.Join("testdata", "test_creds.json")
	v.SetConfigFile(configPath2)
	err = v.ReadInConfig()
	if err != nil {
		t.Fatal(err)

	}
	t.Log("AuthCfg State: ", v)
	err = v.Unmarshal(&tmp2)
	if err != nil {
		t.Fatal(err)

	} else {
		if len(tmp2.Users) <= 0 {
			t.Fatal("No entries found")

		}

		cfg, _ := json.Marshal(tmp2)
		t.Log("Got auth config: ", string(cfg))
	}

	v = viper.New()
	yamlCreds := filepath.Join("testdata", "test_creds.yaml")
	v.SetConfigFile(yamlCreds)
	err = v.ReadInConfig()
	if err != nil {
		t.Fatal("Could not read config")
	}
	t.Log("AuthCfg State: ", v)
	tmp2 = AnchoreAuthConfig{}
	err = v.Unmarshal(&tmp2)
	if err != nil {
		t.Fatal(err)

	} else {
		if len(tmp2.Users) <= 0 {
			t.Fatal("No entries found")

		}

		cfg, _ := json.Marshal(tmp2)
		t.Log("Got auth config: ", string(cfg))
	}

}

func testMetadata() metav1.ObjectMeta {
	labels := map[string]string{
		"labelkey":   "lvalue",
		"labelkey2":  "lvalue2",
		"labelowner": "lsometeam",
	}

	annotations := map[string]string{
		"annotationkey":   "avalue",
		"annotationkey2":  "avalue2",
		"annotationowner": "asometeam",
	}

	return metav1.ObjectMeta{Labels: labels, Annotations: annotations}
}

func assertShouldMatch(t *testing.T, found bool, err error) {
	t.Helper()

	if !found || err != nil {
		t.Fatal("Failed to match")
	}

	t.Log("Matched all properly")
}

func assertShouldNotMatch(t *testing.T, found bool, err error) {
	t.Helper()

	if found || err != nil {
		t.Fatal("Incorrectly matched")
	}

	t.Log("Correctly did not match")
}

func TestMatchObjMetadata(t *testing.T) {
	testCases := []struct {
		name      string
		selector  ResourceSelector
		assertion func(t *testing.T, found bool, err error)
	}{
		{
			name:      "should match anything",
			selector:  ResourceSelector{PodSelectorType, ".*", ".*"},
			assertion: assertShouldMatch,
		},
		{
			name:      "should match key 'label.*'",
			selector:  ResourceSelector{PodSelectorType, "label.*", ".*"},
			assertion: assertShouldMatch,
		},
		{
			name:      "should NOT match key '^label$'",
			selector:  ResourceSelector{PodSelectorType, "^label$", ".*"},
			assertion: assertShouldNotMatch,
		},
		{
			name:      "should match key 'label'",
			selector:  ResourceSelector{PodSelectorType, "label", ".*"},
			assertion: assertShouldMatch,
		},
		{
			name:      "should match key 'labelowner'",
			selector:  ResourceSelector{PodSelectorType, "labelowner", ".*"},
			assertion: assertShouldMatch,
		},
		{
			name:      "should match key 'labelowner' with value 'lsometeam'",
			selector:  ResourceSelector{PodSelectorType, "labelowner", "lsometeam"},
			assertion: assertShouldMatch,
		},
		{
			name:      "should match key 'labelowner' with value 'lsome'",
			selector:  ResourceSelector{PodSelectorType, "labelowner", "lsome"},
			assertion: assertShouldMatch,
		},
		{
			name:      "should match key 'annotation.*'",
			selector:  ResourceSelector{PodSelectorType, "annotation.*", ".*"},
			assertion: assertShouldMatch,
		},
		{
			name:      "should match key 'annotationowner' with value 'asometeam'",
			selector:  ResourceSelector{PodSelectorType, "annotationowner", "asometeam"},
			assertion: assertShouldMatch,
		},
		{
			name:      "should match key 'own' with value '.*team'",
			selector:  ResourceSelector{PodSelectorType, "own", ".*team"},
			assertion: assertShouldMatch,
		},
		{
			name:      "should NOT match key 'notfound'",
			selector:  ResourceSelector{PodSelectorType, "notfound", ".*"},
			assertion: assertShouldNotMatch,
		},
		{
			name:      "should NOT match value 'anotherteam'",
			selector:  ResourceSelector{PodSelectorType, ".*", "anotherteam"},
			assertion: assertShouldNotMatch,
		},
		{
			name:      "should NOT match key 'owner' with value 'anotherteam'",
			selector:  ResourceSelector{PodSelectorType, "owner", "anotherteam"},
			assertion: assertShouldNotMatch,
		},
	}

	metadata := testMetadata()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			found, err := matchObjMetadata(&testCase.selector, &metadata)
			testCase.assertion(t, found, err)
		})
	}
}

func TestMatchImageResource(t *testing.T) {
	testCases := []struct {
		name               string
		selectorValueRegex string
		image              string
		assertion          func(t *testing.T, found bool, err error)
	}{
		{
			name:               "match any image",
			selectorValueRegex: ".*",
			image:              "alpine",
			assertion:          assertShouldMatch,
		},
		{
			name:               "match image with same tag",
			selectorValueRegex: ".*:latest",
			image:              "alpine:latest",
			assertion:          assertShouldMatch,
		},
		{
			name:               "don't match image with different tag",
			selectorValueRegex: ".*:latest",
			image:              "debian:jessie",
			assertion:          assertShouldNotMatch,
		},
		{
			name:               "match image by exact name",
			selectorValueRegex: "alpine",
			image:              "alpine",
			assertion:          assertShouldMatch,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			found, err := matchImageResource(testCase.selectorValueRegex, testCase.image)
			testCase.assertion(t, found, err)
		})
	}
}

func TestValidatePolicyOk(t *testing.T) {
	ts := createTestService()
	defer ts.Close()

	adm := admissionHook{}

	// Valid image, passes policy
	tpod := &v1.Pod{
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name:    "Container1",
				Image:   "alpine",
				Command: []string{"bin/bash", "bin"},
			},
		},
		},
	}

	marshalledPod, err := json.Marshal(tpod)

	if err != nil {
		t.Fatal("Failed marshalling pod spec")
	}

	testConf := ControllerConfiguration{
		ValidatorConfiguration{true, true},
		ts.URL,
		[]PolicySelector{{ResourceSelector{ImageSelectorType, ".*", ".*"}, AnchoreClientConfiguration{"admin", ""}, PolicyGateMode}},
	}

	config = testConf
	authConfig = AnchoreAuthConfig{
		[]AnchoreCredential{AnchoreCredential{"admin", "password"}},
	}

	admSpec := v1beta1.AdmissionRequest{
		UID:         "abc123",
		Kind:        metav1.GroupVersionKind{Group: v1beta1.GroupName, Version: v1beta1.SchemeGroupVersion.Version, Kind: "Pod"},
		Resource:    metav1.GroupVersionResource{Group: metav1.GroupName, Version: "v1", Resource: "pods"},
		SubResource: "someresource",
		Name:        "somename",
		Namespace:   "default",
		Operation:   "CREATE",
		Object:      runtime.RawExtension{Raw: marshalledPod},
	}

	resp := adm.Validate(&admSpec)

	obj, err := json.Marshal(resp)
	if err == nil {
		fmt.Println(string(obj[:]))
	}

	assert.True(t, resp.Allowed)
}

func TestValidatePolicyFail(t *testing.T) {
	ts := createTestService()
	defer ts.Close()

	adm := admissionHook{}

	// Valid image, passes policy
	tpod := &v1.Pod{
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name:    "Container1",
				Image:   "bad-alpine",
				Command: []string{"bin/bash", "bin"},
			},
		},
		},
	}

	marshalledPod, err := json.Marshal(tpod)

	if err != nil {

		t.Fatal("Failed marshalling")
	}

	testConf := ControllerConfiguration{
		ValidatorConfiguration{true, true},
		ts.URL,
		[]PolicySelector{{ResourceSelector{ImageSelectorType, ".*", ".*"}, AnchoreClientConfiguration{"admin", ""}, PolicyGateMode}},
	}

	config = testConf
	authConfig = AnchoreAuthConfig{
		[]AnchoreCredential{AnchoreCredential{"admin", "password"}},
	}

	admSpec := v1beta1.AdmissionRequest{
		UID:         "abc123",
		Kind:        metav1.GroupVersionKind{Group: v1beta1.GroupName, Version: v1beta1.SchemeGroupVersion.Version, Kind: "Pod"},
		Resource:    metav1.GroupVersionResource{Group: metav1.GroupName, Version: "v1", Resource: "pods"},
		SubResource: "someresource",
		Name:        "somename",
		Namespace:   "default",
		Operation:   "CREATE",
		Object:      runtime.RawExtension{Raw: marshalledPod},
	}

	resp := adm.Validate(&admSpec)

	obj, err := json.Marshal(resp)
	if err == nil {
		fmt.Println(string(obj[:]))
	}

	assert.False(t, resp.Allowed)
}

func TestValidatePolicyNotFound(t *testing.T) {
	ts := createTestService()
	defer ts.Close()

	adm := admissionHook{}

	// Valid image, passes policy
	tpod := &v1.Pod{
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name:    "Container1",
				Image:   "ubuntu",
				Command: []string{"bin/bash", "bin"},
			},
		},
		},
	}

	marshalledPod, err := json.Marshal(tpod)

	if err != nil {
		t.Fatal("Failed marshalling")
	}

	testConf := ControllerConfiguration{
		ValidatorConfiguration{true, true},
		ts.URL,
		[]PolicySelector{{ResourceSelector{ImageSelectorType, ".*", ".*"}, AnchoreClientConfiguration{"admin", ""}, PolicyGateMode}},
	}

	config = testConf
	authConfig = AnchoreAuthConfig{
		[]AnchoreCredential{AnchoreCredential{"admin", "password"}},
	}

	admSpec := v1beta1.AdmissionRequest{
		UID:         "abc123",
		Kind:        metav1.GroupVersionKind{Group: v1beta1.GroupName, Version: v1beta1.SchemeGroupVersion.Version, Kind: "Pod"},
		Resource:    metav1.GroupVersionResource{Group: metav1.GroupName, Version: "v1", Resource: "pods"},
		SubResource: "someresource",
		Name:        "somename",
		Namespace:   "default",
		Operation:   "CREATE",
		Object:      runtime.RawExtension{Raw: marshalledPod},
	}

	resp := adm.Validate(&admSpec)

	obj, err := json.Marshal(resp)
	if err == nil {
		fmt.Println(string(obj[:]))
	}

	assert.True(t, !resp.Allowed)
}

func TestValidateAnalyzedOk(t *testing.T) {
	ts := createTestService()
	defer ts.Close()

	adm := admissionHook{}

	// Valid image, passes policy
	tpod := &v1.Pod{
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name:    "Container1",
				Image:   "alpine",
				Command: []string{"bin/bash", "bin"},
			},
		},
		},
	}

	marshalledPod, err := json.Marshal(tpod)

	if err != nil {
		t.Fatal("Failed marshalling")
	}

	testConf := ControllerConfiguration{
		ValidatorConfiguration{true, true},
		ts.URL,
		[]PolicySelector{{ResourceSelector{ImageSelectorType, ".*", ".*"}, AnchoreClientConfiguration{"admin", ""}, AnalysisGateMode}},
	}

	config = testConf
	authConfig = AnchoreAuthConfig{
		[]AnchoreCredential{AnchoreCredential{"admin", "password"}},
	}

	admSpec := v1beta1.AdmissionRequest{
		UID:         "abc123",
		Kind:        metav1.GroupVersionKind{Group: v1beta1.GroupName, Version: v1beta1.SchemeGroupVersion.Version, Kind: "Pod"},
		Resource:    metav1.GroupVersionResource{Group: metav1.GroupName, Version: "v1", Resource: "pods"},
		SubResource: "someresource",
		Name:        "somename",
		Namespace:   "default",
		Operation:   "CREATE",
		Object:      runtime.RawExtension{Raw: marshalledPod},
	}

	resp := adm.Validate(&admSpec)

	obj, err := json.Marshal(resp)
	if err == nil {
		fmt.Println(string(obj[:]))
	}

	assert.True(t, resp.Allowed)
}

func TestValidateAnalyzedFail(t *testing.T) {
	ts := createTestService()
	defer ts.Close()

	adm := admissionHook{}

	// Valid image, passes policy
	tpod := &v1.Pod{
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name:    "Container1",
				Image:   "ubuntu",
				Command: []string{"bin/bash", "bin"},
			},
		},
		},
	}

	marshalledPod, err := json.Marshal(tpod)

	if err != nil {
		t.Fatal("Failed marshalling")
	}

	testConf := ControllerConfiguration{
		ValidatorConfiguration{true, true},
		ts.URL,
		[]PolicySelector{{ResourceSelector{ImageSelectorType, "", ".*"}, AnchoreClientConfiguration{"admin", ""}, AnalysisGateMode}},
	}

	config = testConf
	authConfig = AnchoreAuthConfig{
		[]AnchoreCredential{AnchoreCredential{"admin", "password"}},
	}

	admSpec := v1beta1.AdmissionRequest{
		UID:         "abc123",
		Kind:        metav1.GroupVersionKind{Group: v1beta1.GroupName, Version: v1beta1.SchemeGroupVersion.Version, Kind: "Pod"},
		Resource:    metav1.GroupVersionResource{Group: metav1.GroupName, Version: "v1", Resource: "pods"},
		SubResource: "someresource",
		Name:        "somename",
		Namespace:   "default",
		Operation:   "CREATE",
		Object:      runtime.RawExtension{Raw: marshalledPod},
	}

	resp := adm.Validate(&admSpec)

	obj, err := json.Marshal(resp)
	if err == nil {
		fmt.Println(string(obj[:]))
	}

	assert.False(t, resp.Allowed)
}

func TestValidatePassiveFound(t *testing.T) {
	ts := createTestService()
	defer ts.Close()

	adm := admissionHook{}

	// Valid image, passes policy
	tpod := &v1.Pod{
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name:    "Container1",
				Image:   "alpine",
				Command: []string{"bin/bash", "bin"},
			},
		},
		},
	}

	marshalledPod, err := json.Marshal(tpod)

	if err != nil {
		t.Fatal("Failed marshalling")
	}

	testConf := ControllerConfiguration{
		ValidatorConfiguration{true, true},
		ts.URL,
		[]PolicySelector{{ResourceSelector{ImageSelectorType, "", ".*"}, AnchoreClientConfiguration{"admin", ""}, BreakGlassMode}},
	}

	config = testConf
	authConfig = AnchoreAuthConfig{
		[]AnchoreCredential{AnchoreCredential{"admin", "password"}},
	}

	admSpec := v1beta1.AdmissionRequest{
		UID:         "abc123",
		Kind:        metav1.GroupVersionKind{Group: v1beta1.GroupName, Version: v1beta1.SchemeGroupVersion.Version, Kind: "Pod"},
		Resource:    metav1.GroupVersionResource{Group: metav1.GroupName, Version: "v1", Resource: "pods"},
		SubResource: "someresource",
		Name:        "somename",
		Namespace:   "default",
		Operation:   "CREATE",
		Object:      runtime.RawExtension{Raw: marshalledPod},
	}

	resp := adm.Validate(&admSpec)

	obj, err := json.Marshal(resp)
	if err == nil {
		fmt.Println(string(obj[:]))
	}

	assert.True(t, resp.Allowed)
}

func TestValidatePassiveNotFound(t *testing.T) {
	ts := createTestService()
	defer ts.Close()

	adm := admissionHook{}

	// Valid image, passes policy
	tpod := &v1.Pod{
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name:    "Container1",
				Image:   "ubuntu",
				Command: []string{"bin/bash", "bin"},
			},
		},
		},
	}

	marshalledPod, err := json.Marshal(tpod)

	if err != nil {
		t.Fatal("Failed marshalling")
	}

	testConf := ControllerConfiguration{
		ValidatorConfiguration{true, true},
		ts.URL,
		[]PolicySelector{{ResourceSelector{ImageSelectorType, "", ".*"}, AnchoreClientConfiguration{"admin", ""}, BreakGlassMode}},
	}

	config = testConf
	authConfig = AnchoreAuthConfig{
		[]AnchoreCredential{AnchoreCredential{"admin", "password"}},
	}

	admSpec := v1beta1.AdmissionRequest{
		UID:         "abc123",
		Kind:        metav1.GroupVersionKind{Group: v1beta1.GroupName, Version: v1beta1.SchemeGroupVersion.Version, Kind: "Pod"},
		Resource:    metav1.GroupVersionResource{Group: metav1.GroupName, Version: "v1", Resource: "pods"},
		SubResource: "someresource",
		Name:        "somename",
		Namespace:   "default",
		Operation:   "CREATE",
		Object:      runtime.RawExtension{Raw: marshalledPod},
	}

	resp := adm.Validate(&admSpec)

	obj, err := json.Marshal(resp)
	if err == nil {
		fmt.Println(string(obj[:]))
	}

	assert.True(t, resp.Allowed)
}

func createTestService() *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/images" {
			switch r.URL.Query().Get("fulltag") {
			case "docker.io/alpine:latest", "docker.io/alpine", "alpine":
				fmt.Fprintln(w, AlpineImageLookup)
				return
			case "docker.io/bad-alpine:latest", "docker.io/bad-alpine", "bad-alpine":
				fmt.Fprintln(w, BadAlpineImageLookup)
				return
			}

			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, ImageLookupError)
			return
		}

		switch r.URL.Path {
		case "/images/sha256:02892826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b/check":
			fmt.Fprintln(w, GoodPassResponse)
			return
		case "/images/sha256:11111826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b/check":
			fmt.Fprintln(w, GoodFailResponse)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, ImageNotFound)
	}))

	return ts
}

const GoodPassResponse = `
[
  {
    "sha256:02892826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b": {
      "docker.io/alpine:latest": [
        {
          "detail": {},
          "last_evaluation": "2018-12-03T17:46:13Z",
          "policyId": "2c53a13c-1765-11e8-82ef-23527761d060",
          "status": "pass"
        }
      ]
    }
  }
]
`

const GoodFailResponse = `
[
  {
    "sha256:02892826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b": {
      "docker.io/alpine:latest": [
        {
          "detail": {},
          "last_evaluation": "2018-12-03T17:46:13Z",
          "policyId": "2c53a13c-1765-11e8-82ef-23527761d060",
          "status": "fail"
        }
      ]
    }
  }
]
`

const ImageNotFound = `
{  
  "message": "could not get image record from anchore"
}
`

const AlpineImageLookup = `
[
  {
    "analysis_status": "analyzed",
    "analyzed_at": "2018-12-03T18:24:54Z",
    "annotations": {},
    "created_at": "2018-12-03T18:24:43Z",
    "imageDigest": "sha256:02892826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b",
    "image_content": {
      "metadata": {
        "arch": "amd64",
        "distro": "alpine",
        "distro_version": "3.8.1",
        "dockerfile_mode": "Guessed",
        "image_size": 2206931,
        "layer_count": 1
      }
    },
    "image_detail": [
      {
        "created_at": "2018-12-03T18:24:43Z",
        "digest": "sha256:02892826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b",
        "dockerfile": "RlJPTSBzY3JhdGNoCkFERCBmaWxlOjI1YzEwYjFkMWI0MWQ0NmExODI3YWQwYjBkMjM4OWMyNGRmNmQzMTQzMDAwNWZmNGU5YTJkODRlYTIzZWJkNDIgaW4gLyAKQ01EIFsiL2Jpbi9zaCJdCg==",
        "fulldigest": "docker.io/alpine@sha256:02892826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b",
        "fulltag": "docker.io/alpine:latest",
        "imageDigest": "sha256:02892826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b",
        "imageId": "196d12cf6ab19273823e700516e98eb1910b03b17840f9d5509f03858484d321",
        "last_updated": "2018-12-03T18:24:54Z",
        "registry": "docker.io",
        "repo": "alpine",
        "tag": "latest",
        "tag_detected_at": "2018-12-03T18:24:43Z",
        "userId": "admin"
      }
    ],
    "image_status": "active",
    "image_type": "docker",
    "last_updated": "2018-12-03T18:24:54Z",
    "parentDigest": "sha256:621c2f39f8133acb8e64023a94dbdf0d5ca81896102b9e57c0dc184cadaf5528",
    "userId": "admin"
  }
]
`

const BadAlpineImageLookup = `
[
  {
    "analysis_status": "analyzed",
    "analyzed_at": "2018-12-03T18:24:54Z",
    "annotations": {},
    "created_at": "2018-12-03T18:24:43Z",
    "imageDigest": "sha256:11111826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b",
    "image_content": {
      "metadata": {
        "arch": "amd64",
        "distro": "alpine",
        "distro_version": "3.8.1",
        "dockerfile_mode": "Guessed",
        "image_size": 2206931,
        "layer_count": 1
      }
    },
    "image_detail": [
      {
        "created_at": "2018-12-03T18:24:43Z",
        "digest": "sha256:11111826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b",
        "dockerfile": "RlJPTSBzY3JhdGNoCkFERCBmaWxlOjI1YzEwYjFkMWI0MWQ0NmExODI3YWQwYjBkMjM4OWMyNGRmNmQzMTQzMDAwNWZmNGU5YTJkODRlYTIzZWJkNDIgaW4gLyAKQ01EIFsiL2Jpbi9zaCJdCg==",
        "fulldigest": "docker.io/bad-alpine@sha256:11111826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b",
        "fulltag": "docker.io/bad-alpine:latest",
        "imageDigest": "sha256:11111826401a9d18f0ea01f8a2f35d328ef039db4e1edcc45c630314a0457d5b",
        "imageId": "196d12cf6ab19273823e700516e98eb1910b03b17840f9d5509f03858484d321",
        "last_updated": "2018-12-03T18:24:54Z",
        "registry": "docker.io",
        "repo": "bad-alpine",
        "tag": "latest",
        "tag_detected_at": "2018-12-03T18:24:43Z",
        "userId": "admin"
      }
    ],
    "image_status": "active",
    "image_type": "docker",
    "last_updated": "2018-12-03T18:24:54Z",
    "parentDigest": "sha256:621c2f39f8133acb8e64023a94dbdf0d5ca81896102b9e57c0dc184cadaf5528",
    "userId": "admin"
  }
]
`

const ImageLookupError = `{"detail": {}, "httpcode": 404, "message": "image data not found in DB" }`

func TestLookupImage(t *testing.T) {
	t.Log("Testing image lookup handling")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/images" {
			fmt.Fprint(w, "Ok")

		} else {
			switch r.URL.Query().Get("fulltag") {
			case "docker.io/alpine:latest":
				fmt.Fprintln(w, AlpineImageLookup)
			default:
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, ImageLookupError)
			}
		}
	}))

	defer ts.Close()

	t.Log(fmt.Sprintf("URL: %s", ts.URL))

	client, authCtx, err := initClient("admin", "foobar", ts.URL)

	if err != nil {
		t.Fatal(err)
	}

	localOpts := anchore.ListImagesOpts{}

	var calls = [][]string{{"docker.io/alpine:latest", "analyzed"}, {"docker.io/alpine:3.8", "notfound"}}
	var result string

	for _, item := range calls {
		t.Log(fmt.Sprintf("Checking %s", item))
		localOpts.Fulltag = optional.NewString(item[0])
		imageListing, _, err := client.ImagesApi.ListImages(authCtx, &localOpts)
		if err != nil {
			if item[1] == "notfound" {
				t.Log("Expected error response from server: ", err)
			} else {
				t.Fatal("Did not expect an error")
			}
			continue
		}

		fmt.Printf("Images: %v\n", imageListing)
		result = imageListing[0].AnalysisStatus
		fmt.Printf("Result: %s\n", result)
		if result != item[1] {
			t.Fatal(fmt.Sprintf("Expected %s but got %s", item[1], result))

		}
	}
}

/*
Test the CheckImage function against some fake responses
 - Successful policy eval
 - Error during policy eval
 - Image not found
 - Bundle not found

*/
func TestCheckImage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/images/goodfail/check":
			fmt.Fprintln(w, GoodFailResponse)
		case "/images/goodpass/check":
			fmt.Fprintln(w, GoodPassResponse)
		case "/images/imagenotfound/check":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, ImageNotFound)
		case "/images/policynotfound/check":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, ImageNotFound)
		default:
			fmt.Fprint(w, r.URL.Path)
		}
	}))

	defer ts.Close()

	t.Log(fmt.Sprintf("URL: %s", ts.URL))

	client, authCtx, err := initClient("admin", "foobar", ts.URL)

	if err != nil {
		t.Fatal(err)
	}

	localOpts := anchore.GetImagePolicyCheckOpts{}

	var calls = [][]string{{"goodpass", "pass"}, {"goodfail", "fail"}, {"imagenotfound", "notfound"}, {"policynotfound", "notfound"}}
	var result string

	for _, item := range calls {

		t.Log(fmt.Sprintf("Checking %s", item))
		policyEvaluations, _, err := client.ImagesApi.GetImagePolicyCheck(authCtx, item[0], "docker.io/alpine", &localOpts)
		if err != nil {
			if item[1] == "notfound" {
				t.Log("Expected error response from server: ", err)

			} else {
				t.Fatal(t, "Did not expect an error")
			}
			continue
		}

		fmt.Printf("Policy evaluation: %s\n", policyEvaluations)
		result = findResult(policyEvaluations[0])
		fmt.Printf("Result: %s\n", result)
		if result != item[1] {
			t.Fatal(fmt.Sprintf("Expected %s but got %s", item[1], result))
		}
	}

}
