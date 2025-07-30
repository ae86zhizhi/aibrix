=========================
KV Event Synchronization
=========================

Overview
--------
KV Event Synchronization enables multiple vLLM instances to share key-value cache states, improving prefix cache hit rates and reducing redundant computation. This feature allows the AIBrix gateway to make intelligent routing decisions based on real-time cache state from vLLM engines.

Requirements
------------
- vLLM version 0.7.0 or later
- AIBrix version 0.4.0 or later
- ZMQ library support in vLLM image
- Remote tokenizer enabled in gateway (prerequisite)

.. note::
   KV event sync requires remote tokenizer to ensure consistent tokenization between gateway and vLLM.

Quick Start
-----------

New Deployments
~~~~~~~~~~~~~~~
Use the provided templates with KV events pre-configured:

.. code-block:: bash

   kubectl apply -f samples/quickstart/model-with-kv-events.yaml

Existing Deployments
~~~~~~~~~~~~~~~~~~~~
Run the migration script:

.. code-block:: bash

   ./migration/enable-kv-events.sh default my-model

Configure Gateway
~~~~~~~~~~~~~~~~~
Ensure gateway has remote tokenizer enabled:

.. code-block:: bash

   kubectl set env deployment/aibrix-gateway-plugins -n aibrix-system \
     AIBRIX_USE_REMOTE_TOKENIZER=true \
     AIBRIX_KV_EVENT_SYNC_ENABLED=true \
     AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE=remote \
     AIBRIX_REMOTE_TOKENIZER_ENGINE=vllm \
     AIBRIX_REMOTE_TOKENIZER_ENDPOINT=http://vllm-service.default:8000

Verify Configuration
~~~~~~~~~~~~~~~~~~~~
Check that events are being published:

.. code-block:: bash

   kubectl logs deployment/my-model | grep "KV cache events"

Check gateway configuration:

.. code-block:: bash

   kubectl logs -n aibrix-system deployment/aibrix-gateway-plugins | grep "KV event"

Configuration Options
---------------------

vLLM Options
~~~~~~~~~~~~

.. list-table::
   :header-rows: 1
   :widths: 30 20 50

   * - Option
     - Default
     - Description
   * - ``--enable-kv-cache-events``
     - false
     - Enable KV event publishing
   * - ``--kv-events-buffer-steps``
     - 10000
     - Number of events to buffer for replay
   * - ``--kv-events-topic``
     - ""
     - Topic prefix for events

Gateway Requirements
~~~~~~~~~~~~~~~~~~~~

.. list-table::
   :header-rows: 1
   :widths: 40 15 45

   * - Environment Variable
     - Required
     - Description
   * - ``AIBRIX_USE_REMOTE_TOKENIZER``
     - Yes
     - Must be "true" for KV sync
   * - ``AIBRIX_KV_EVENT_SYNC_ENABLED``
     - Yes
     - Enable KV event consumption
   * - ``AIBRIX_REMOTE_TOKENIZER_ENDPOINT``
     - Yes
     - vLLM service endpoint

Troubleshooting
---------------

Events Not Publishing
~~~~~~~~~~~~~~~~~~~~~
1. Check vLLM logs for errors
2. Verify ZMQ ports are accessible
3. Ensure labels are correctly set

High Memory Usage
~~~~~~~~~~~~~~~~~
- Reduce buffer steps
- Enable event compression (if supported)

Performance Impact
~~~~~~~~~~~~~~~~~~
- KV events add <1% CPU overhead
- Network bandwidth: ~1MB/s per pod at high load

Architecture
------------
The KV event synchronization system consists of the following components:

* **vLLM Instances**: Publish KV cache events via ZMQ
* **AIBrix Cache**: Subscribes to events and maintains global state
* **Gateway Router**: Uses cache state for intelligent routing decisions

.. note::
   Only gateway-plugins and kvcache-watcher components use KV event sync. Controller-manager and metadata-service do not require ZMQ dependencies.

See Also
--------
- :doc:`/deployment/kv-event-sync-setup`
- :doc:`/api/kv-event-sync`
- :doc:`/migration/enable-kv-events`
- :doc:`/testing/kv-event-sync-e2e`