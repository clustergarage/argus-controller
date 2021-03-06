// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha1 "clustergarage.io/argus-controller/pkg/apis/arguscontroller/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeArgusWatchers implements ArgusWatcherInterface
type FakeArgusWatchers struct {
	Fake *FakeArguscontrollerV1alpha1
	ns   string
}

var arguswatchersResource = schema.GroupVersionResource{Group: "arguscontroller.clustergarage.io", Version: "v1alpha1", Resource: "arguswatchers"}

var arguswatchersKind = schema.GroupVersionKind{Group: "arguscontroller.clustergarage.io", Version: "v1alpha1", Kind: "ArgusWatcher"}

// Get takes name of the argusWatcher, and returns the corresponding argusWatcher object, and an error if there is any.
func (c *FakeArgusWatchers) Get(name string, options v1.GetOptions) (result *v1alpha1.ArgusWatcher, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(arguswatchersResource, c.ns, name), &v1alpha1.ArgusWatcher{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ArgusWatcher), err
}

// List takes label and field selectors, and returns the list of ArgusWatchers that match those selectors.
func (c *FakeArgusWatchers) List(opts v1.ListOptions) (result *v1alpha1.ArgusWatcherList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(arguswatchersResource, arguswatchersKind, c.ns, opts), &v1alpha1.ArgusWatcherList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ArgusWatcherList{ListMeta: obj.(*v1alpha1.ArgusWatcherList).ListMeta}
	for _, item := range obj.(*v1alpha1.ArgusWatcherList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested argusWatchers.
func (c *FakeArgusWatchers) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(arguswatchersResource, c.ns, opts))

}

// Create takes the representation of a argusWatcher and creates it.  Returns the server's representation of the argusWatcher, and an error, if there is any.
func (c *FakeArgusWatchers) Create(argusWatcher *v1alpha1.ArgusWatcher) (result *v1alpha1.ArgusWatcher, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(arguswatchersResource, c.ns, argusWatcher), &v1alpha1.ArgusWatcher{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ArgusWatcher), err
}

// Update takes the representation of a argusWatcher and updates it. Returns the server's representation of the argusWatcher, and an error, if there is any.
func (c *FakeArgusWatchers) Update(argusWatcher *v1alpha1.ArgusWatcher) (result *v1alpha1.ArgusWatcher, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(arguswatchersResource, c.ns, argusWatcher), &v1alpha1.ArgusWatcher{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ArgusWatcher), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeArgusWatchers) UpdateStatus(argusWatcher *v1alpha1.ArgusWatcher) (*v1alpha1.ArgusWatcher, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(arguswatchersResource, "status", c.ns, argusWatcher), &v1alpha1.ArgusWatcher{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ArgusWatcher), err
}

// Delete takes name of the argusWatcher and deletes it. Returns an error if one occurs.
func (c *FakeArgusWatchers) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(arguswatchersResource, c.ns, name), &v1alpha1.ArgusWatcher{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeArgusWatchers) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(arguswatchersResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.ArgusWatcherList{})
	return err
}

// Patch applies the patch and returns the patched argusWatcher.
func (c *FakeArgusWatchers) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.ArgusWatcher, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(arguswatchersResource, c.ns, name, data, subresources...), &v1alpha1.ArgusWatcher{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ArgusWatcher), err
}
