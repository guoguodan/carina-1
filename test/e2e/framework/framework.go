package framework

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apiextcs "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

var (
	// KubectlPath defines the full path of the kubectl binary
	KubectlPath = "/usr/local/bin/kubectl"
)

type Framework struct {
	BaseName string
	// A Kubernetes and Service Catalog client
	KubeClientSet          kubernetes.Interface
	KubeConfig             *restclient.Config
	APIExtensionsClientSet apiextcs.Interface
	Namespace              string
}

// NewDefaultFramework makes a new framework and sets up a BeforeEach/AfterEach for
// you (you can write additional before/after each functions).
func NewDefaultFramework(baseName string) *Framework {
	defer ginkgo.GinkgoRecover()

	f := &Framework{
		BaseName: baseName,
	}

	ginkgo.BeforeEach(f.BeforeEach)
	ginkgo.AfterEach(f.AfterEach)

	return f
}

// NewSimpleFramework makes a new framework that allows the usage of a namespace
// for arbitraty tests.
func NewSimpleFramework(baseName string) *Framework {
	defer ginkgo.GinkgoRecover()

	f := &Framework{
		BaseName: baseName,
	}

	ginkgo.BeforeEach(f.CreateEnvironment)
	ginkgo.AfterEach(f.DestroyEnvironment)

	return f
}

func (f *Framework) CreateEnvironment() {
	var err error

	if f.KubeClientSet == nil {
		f.KubeConfig, err = GetConfig()
		assert.Nil(ginkgo.GinkgoT(), err, "loading a kubernetes client configuration")

		// TODO: remove after k8s v1.22
		f.KubeConfig.WarningHandler = restclient.NoWarnings{}

		f.KubeClientSet, err = kubernetes.NewForConfig(f.KubeConfig)
		assert.Nil(ginkgo.GinkgoT(), err, "creating a kubernetes client")

		// TODO 检查Carina相关CRD是否安装
	}

	f.Namespace, err = CreateKubeNamespace(f.BaseName, f.KubeClientSet)
	assert.Nil(ginkgo.GinkgoT(), err, "creating namespace")
}

func (f *Framework) DestroyEnvironment() {
	go func() {
		defer ginkgo.GinkgoRecover()
		err := DeleteKubeNamespace(f.KubeClientSet, f.Namespace)
		assert.Nil(ginkgo.GinkgoT(), err, "deleting namespace %v", f.Namespace)
	}()
}

// BeforeEach gets a client and makes a namespace.
func (f *Framework) BeforeEach() {
	f.CreateEnvironment()
}

// AfterEach deletes the namespace, after reading its events.
func (f *Framework) AfterEach() {
	defer f.DestroyEnvironment()
}

// Craina wrapper function for ginkgo describe. Adds namespacing.
func CrainaDescribe(text string, body func()) bool {
	return ginkgo.Describe(text, body)
}

// EnsurePvc creates an pvc object and returns it, throws error if it already exists.
func (f *Framework) EnsurePvc(pvc *corev1.PersistentVolumeClaim) *corev1.PersistentVolumeClaim {

	err := createPvcWithRetries(f.KubeClientSet, pvc.Namespace, pvc)
	assert.Nil(ginkgo.GinkgoT(), err, "creating pvc")

	pvcResult := f.GetPvc(pvc.Namespace, pvc.Name)
	return pvcResult
}

func createPvcWithRetries(c kubernetes.Interface, namespace string, obj *corev1.PersistentVolumeClaim) error {
	if obj == nil {
		return fmt.Errorf("object provided to create is empty")
	}
	createFunc := func() (bool, error) {
		_, err := c.CoreV1().PersistentVolumeClaims(namespace).Create(context.TODO(), obj, metav1.CreateOptions{})
		if err == nil {
			return true, nil
		}
		if k8sErrors.IsAlreadyExists(err) {
			return false, err
		}
		if isRetryableAPIError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to create object with non-retriable error: %v", err)
	}

	return retryWithExponentialBackOff(createFunc)
}
