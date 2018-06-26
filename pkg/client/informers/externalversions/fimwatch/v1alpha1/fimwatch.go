/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	time "time"

	fimwatch_v1alpha1 "github.com/clustergarage/fim-k8s/pkg/apis/fimwatch/v1alpha1"
	versioned "github.com/clustergarage/fim-k8s/pkg/client/clientset/versioned"
	internalinterfaces "github.com/clustergarage/fim-k8s/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/clustergarage/fim-k8s/pkg/client/listers/fimwatch/v1alpha1"
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
				return client.FimV1alpha1().FimWatches(namespace).List(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.FimV1alpha1().FimWatches(namespace).Watch(options)
			},
		},
		&fimwatch_v1alpha1.FimWatch{},
		resyncPeriod,
		indexers,
	)
}

func (f *fimWatchInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredFimWatchInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *fimWatchInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&fimwatch_v1alpha1.FimWatch{}, f.defaultInformer)
}

func (f *fimWatchInformer) Lister() v1alpha1.FimWatchLister {
	return v1alpha1.NewFimWatchLister(f.Informer().GetIndexer())
}