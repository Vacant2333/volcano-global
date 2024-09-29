/*
Copyright 2024 The Volcano Authors.

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

package cache

import (
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"k8s.io/klog/v2"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/scheme"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano-global/pkg/dispatcher/api"
	"volcano.sh/volcano-global/pkg/utils"
)

func (dc *DispatcherCache) addQueue(obj interface{}) {
	queue := convertToQueue(obj)
	if queue == nil {
		return
	}

	// Convert the queue from v1beta1 to v1
	v1queue := &scheduling.Queue{}
	if err := scheme.Scheme.Convert(queue, v1queue, nil); err != nil {
		klog.Errorf("Failed to convert queue from %T to %T", queue, v1queue)
		return
	}

	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	dc.queues[queue.Name] = schedulingapi.NewQueueInfo(v1queue)
}

func (dc *DispatcherCache) deleteQueue(obj interface{}) {
	queue := convertToQueue(obj)
	if queue == nil {
		return
	}
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	delete(dc.queues, queue.Name)
}

func (dc *DispatcherCache) updateQueue(oldObj, newObj interface{}) {
	oldQueue := convertToQueue(oldObj)
	newQueue := convertToQueue(newObj)
	if oldQueue == nil || newQueue == nil {
		return
	}

	dc.deleteQueue(oldQueue)
	dc.addQueue(newQueue)
}

func (dc *DispatcherCache) addPodGroup(obj interface{}) {
	pg := convertToPodGroup(obj)
	if pg == nil {
		return
	}
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	if dc.podGroups[pg.Namespace] == nil {
		dc.podGroups[pg.Namespace] = map[string]*schedulingv1beta1.PodGroup{
			pg.Name: pg,
		}
	} else {
		dc.podGroups[pg.Namespace][pg.Name] = pg
	}
}

func (dc *DispatcherCache) deletePodGroup(obj interface{}) {
	pg := convertToPodGroup(obj)
	if pg == nil {
		return
	}
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	if dc.podGroups[pg.Namespace] == nil {
		klog.Errorf("Failed to delete PodGroup <%s/%s>, the PodGroup's "+
			"Namespace should is not in the cache.", pg.Namespace, pg.Name)
		return
	} else {
		delete(dc.podGroups[pg.Namespace], pg.Name)
	}
}

func (dc *DispatcherCache) updatePodGroup(oldObj, newObj interface{}) {
	oldPg := convertToPodGroup(oldObj)
	newPg := convertToPodGroup(newObj)
	if oldPg == nil || newPg == nil {
		return
	}

	dc.deletePodGroup(oldPg)
	dc.addPodGroup(newPg)
}

func (dc *DispatcherCache) addPriorityClass(obj interface{}) {
	pc := convertToPriorityClass(obj)
	if pc == nil {
		return
	}
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	if pc.GlobalDefault {
		klog.V(3).Infof("Set default PriorityClass to <%s>, Priority <%d>.", pc.Name, pc.Value)
		dc.defaultPriorityClass = pc
	}

	dc.priorityClasses[pc.Name] = pc
}

func (dc *DispatcherCache) deletePriorityClass(obj interface{}) {
	pc := convertToPriorityClass(obj)
	if pc == nil {
		return
	}
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	if pc.GlobalDefault {
		klog.V(5).Infof("Delete default PriorityClass <%s>, Priority <%d>.", pc.Name, pc.Value)
		dc.defaultPriorityClass = nil
	}
	delete(dc.priorityClasses, pc.Name)
}

func (dc *DispatcherCache) updatePriorityClass(oldObj, newObj interface{}) {
	oldPc := convertToPriorityClass(oldObj)
	newPc := convertToPriorityClass(newObj)
	if oldPc == nil || newPc == nil {
		return
	}
	dc.deletePriorityClass(oldPc)
	dc.addPriorityClass(newPc)
}

func (dc *DispatcherCache) addResourceBinding(obj interface{}) {
	rb := convertToResourceBinding(obj)
	if rb == nil {
		return
	}

	// Check if its workload, skip add to cache if not.
	isWorkload, err := utils.IsWorkload(rb.Spec.Resource)
	if err != nil {
		klog.Errorf("Failed to check ResourceBinding <%s/%s> if workload, stop add it to cache, err: %v",
			rb.Namespace, rb.Name, err)
		return
	}
	if !isWorkload {
		klog.V(3).Infof("ResourceBinding <%s/%s> is not a workload, skip add it to cache.",
			rb.Namespace, rb.Name)
		return
	}

	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	// Add the ResourceBinding to cache.
	if dc.resourceBindings[rb.Namespace] == nil {
		dc.resourceBindings[rb.Namespace] = map[string]*workv1alpha2.ResourceBinding{
			rb.Name: rb,
		}
	} else {
		dc.resourceBindings[rb.Namespace][rb.Name] = rb
	}

	// Build the ResourceBindingInfo, the other elements will set when Snapshot.
	newResourceBindingInfo := &api.ResourceBindingInfo{
		ResourceBinding: rb,
		ResourceUID:     rb.Spec.Resource.UID,
		DispatchStatus:  api.UnSuspended,
	}
	// Currently, our failurePolicy is set to Fail, which ensures that no unexpected ResourceBindings will exist.
	// When a ResourceBinding is created, it will definitely be updated to Suspend, so we don't need to check the Status.
	if rb.Spec.Suspend {
		newResourceBindingInfo.DispatchStatus = api.Suspended
	}

	if dc.resourceBindingInfos[rb.Namespace] == nil {
		dc.resourceBindingInfos[rb.Namespace] = map[string]*api.ResourceBindingInfo{
			rb.Name: newResourceBindingInfo,
		}
	} else {
		dc.resourceBindingInfos[rb.Namespace][rb.Name] = newResourceBindingInfo
	}
}

func (dc *DispatcherCache) deleteResourceBinding(obj interface{}) {
	rb := convertToResourceBinding(obj)
	if rb == nil {
		return
	}
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	if dc.resourceBindings[rb.Namespace] == nil {
		klog.Errorf("Failed to delete ResourceBinding <%s/%s>, the Resourcebinding's "+
			"Namespace is not in the cache.", rb.Namespace, rb.Name)
		return
	} else {
		delete(dc.resourceBindings[rb.Namespace], rb.Name)
		delete(dc.resourceBindingInfos[rb.Namespace], rb.Name)
	}
}

func (dc *DispatcherCache) updateResourceBinding(oldObj, newObj interface{}) {
	oldRb := convertToResourceBinding(oldObj)
	newRb := convertToResourceBinding(newObj)
	if oldRb == nil || newRb == nil {
		return
	}

	dc.deleteResourceBinding(oldRb)
	dc.addResourceBinding(newRb)
}