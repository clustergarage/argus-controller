// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	time "time"

	fimcontroller_v1alpha1 "clustergarage.io/fim-k8s/pkg/apis/fimcontroller/v1alpha1"
	versioned "clustergarage.io/fim-k8s/pkg/client/clientset/versioned"
	internalinterfaces "clustergarage.io/fim-k8s/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "clustergarage.io/fim-k8s/pkg/client/listers/fimcontroller/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// FimWatchInformer provides access to a shared informer and lister for
// FimWatches.
type FimWatchInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.FimWatchLister
}

type fimWatchInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewFimWatchInformer constructs a new informer for FimWatch type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFimWatchInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredFimWatchInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredFimWatchInformer constructs a new informer for FimWatch type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredFimWatchInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.FimcontrollerV1alpha1().FimWatches(namespace).List(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.FimcontrollerV1alpha1().FimWatches(namespace).Watch(options)
			},
		},
		&fimcontroller_v1alpha1.FimWatch{},
		resyncPeriod,
		indexers,
	)
}

func (f *fimWatchInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredFimWatchInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *fimWatchInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&fimcontroller_v1alpha1.FimWatch{}, f.defaultInformer)
}

func (f *fimWatchInformer) Lister() v1alpha1.FimWatchLister {
	return v1alpha1.NewFimWatchLister(f.Informer().GetIndexer())
}
