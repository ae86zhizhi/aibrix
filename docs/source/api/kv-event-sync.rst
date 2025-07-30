===========================
KV Event Sync API Reference
===========================

This document provides a comprehensive API reference for the KV Event Synchronization system in AIBrix.

Event Types
-----------

BlockStoredEvent
~~~~~~~~~~~~~~~~

Published when new KV cache blocks are stored.

.. code-block:: go

    type BlockStoredEvent struct {
        SequenceID    int      `msgpack:"sequence_id"`
        BlockNumbers  []int    `msgpack:"block_numbers"`
        BlockTokenIDs [][]int  `msgpack:"block_token_ids"`
    }

**Fields:**

- ``SequenceID``: Unique identifier for the sequence
- ``BlockNumbers``: List of block numbers that were stored
- ``BlockTokenIDs``: Token IDs for each block (2D array)

**Example:**

.. code-block:: json

    {
        "sequence_id": 12345,
        "block_numbers": [0, 1, 2],
        "block_token_ids": [[101, 102], [103, 104], [105, 106]]
    }

BlockRemovedEvent
~~~~~~~~~~~~~~~~~

Published when blocks are removed from cache.

.. code-block:: go

    type BlockRemovedEvent struct {
        SequenceID   int   `msgpack:"sequence_id"`
        BlockNumbers []int `msgpack:"block_numbers"`
    }

**Fields:**

- ``SequenceID``: Unique identifier for the sequence
- ``BlockNumbers``: List of block numbers that were removed

**Example:**

.. code-block:: json

    {
        "sequence_id": 12345,
        "block_numbers": [0, 1]
    }

RequestFinishedEvent
~~~~~~~~~~~~~~~~~~~~

Published when a request completes processing.

.. code-block:: go

    type RequestFinishedEvent struct {
        RequestID  string `msgpack:"request_id"`
        SequenceID int    `msgpack:"sequence_id"`
        Finished   bool   `msgpack:"finished"`
    }

**Fields:**

- ``RequestID``: Unique request identifier
- ``SequenceID``: Associated sequence ID
- ``Finished``: Whether the request completed successfully

Configuration Reference
-----------------------

Environment Variables
~~~~~~~~~~~~~~~~~~~~~

Core Configuration
^^^^^^^^^^^^^^^^^^

.. list-table::
   :header-rows: 1
   :widths: 40 20 40

   * - Variable
     - Default
     - Description
   * - ``AIBRIX_KV_EVENT_SYNC_ENABLED``
     - false
     - Enable KV event synchronization
   * - ``AIBRIX_USE_REMOTE_TOKENIZER``
     - false
     - Must be true for KV sync to work
   * - ``AIBRIX_PREFIX_CACHE_TOKENIZER_TYPE``
     - local
     - Must be "remote" for KV sync
   * - ``AIBRIX_REMOTE_TOKENIZER_ENDPOINT``
     - ""
     - vLLM service endpoint (required)

ZMQ Configuration
^^^^^^^^^^^^^^^^^

.. list-table::
   :header-rows: 1
   :widths: 40 20 40

   * - Variable
     - Default
     - Description
   * - ``AIBRIX_ZMQ_POLL_TIMEOUT``
     - 100ms
     - ZMQ poll timeout duration
   * - ``AIBRIX_ZMQ_REPLAY_TIMEOUT``
     - 5s
     - Timeout for replay requests
   * - ``AIBRIX_ZMQ_RECV_TIMEOUT``
     - 1s
     - Receive timeout for messages
   * - ``AIBRIX_ZMQ_SEND_TIMEOUT``
     - 1s
     - Send timeout for messages
   * - ``AIBRIX_ZMQ_LINGER``
     - 0
     - ZMQ socket linger period

Performance Tuning
^^^^^^^^^^^^^^^^^^

.. list-table::
   :header-rows: 1
   :widths: 40 20 40

   * - Variable
     - Default
     - Description
   * - ``AIBRIX_KV_EVENT_BUFFER_SIZE``
     - 10000
     - Maximum events to buffer
   * - ``AIBRIX_KV_EVENT_BATCH_SIZE``
     - 100
     - Events to process per batch
   * - ``AIBRIX_KV_EVENT_WORKER_COUNT``
     - 4
     - Number of worker goroutines
   * - ``AIBRIX_KV_EVENT_QUEUE_SIZE``
     - 1000
     - Internal queue size

vLLM Configuration
~~~~~~~~~~~~~~~~~~

Command Line Arguments
^^^^^^^^^^^^^^^^^^^^^^

.. list-table::
   :header-rows: 1
   :widths: 40 20 40

   * - Argument
     - Default
     - Description
   * - ``--enable-kv-cache-events``
     - false
     - Enable KV event publishing
   * - ``--kv-events-publisher``
     - zmq
     - Event publisher type
   * - ``--kv-events-endpoint``
     - ``tcp://*:5557``
     - ZMQ bind endpoint
   * - ``--kv-events-replay-endpoint``
     - ``tcp://*:5558``
     - Replay service endpoint
   * - ``--kv-events-buffer-steps``
     - 10000
     - Events to buffer for replay
   * - ``--kv-events-topic``
     - ""
     - Topic prefix for events

Go API Reference
----------------

Event Manager Interface
~~~~~~~~~~~~~~~~~~~~~~~

.. code-block:: go

    type EventManager interface {
        // Start begins event processing
        Start(ctx context.Context) error
        
        // Stop gracefully shuts down event processing
        Stop() error
        
        // GetSyncIndexer returns the sync indexer
        GetSyncIndexer() SyncIndexer
        
        // IsHealthy checks if the manager is healthy
        IsHealthy() bool
    }

Sync Indexer Interface
~~~~~~~~~~~~~~~~~~~~~~

.. code-block:: go

    type SyncIndexer interface {
        // GetCachedTokens returns cached tokens for a sequence
        GetCachedTokens(sequenceID int) ([]int, error)
        
        // GetAllCachedSequences returns all cached sequence IDs
        GetAllCachedSequences() ([]int, error)
        
        // GetCacheStats returns cache statistics
        GetCacheStats() CacheStats
        
        // Clear removes all cached data
        Clear() error
    }

Cache Stats Structure
~~~~~~~~~~~~~~~~~~~~~

.. code-block:: go

    type CacheStats struct {
        TotalSequences   int     `json:"total_sequences"`
        TotalBlocks      int     `json:"total_blocks"`
        TotalTokens      int     `json:"total_tokens"`
        HitRate          float64 `json:"hit_rate"`
        EvictionCount    int64   `json:"eviction_count"`
        LastUpdated      int64   `json:"last_updated"`
    }

Python API Reference
--------------------

Event Publisher (vLLM)
~~~~~~~~~~~~~~~~~~~~~~

.. code-block:: python

    class KVCacheEventPublisher:
        """Publisher for KV cache events in vLLM."""
        
        def __init__(self, endpoint: str, topic: str = ""):
            """Initialize the event publisher.
            
            Args:
                endpoint: ZMQ endpoint to bind to
                topic: Optional topic prefix for events
            """
        
        def publish_block_stored(self, 
                               sequence_id: int,
                               block_numbers: List[int],
                               block_token_ids: List[List[int]]):
            """Publish a block stored event."""
        
        def publish_block_removed(self,
                                sequence_id: int,
                                block_numbers: List[int]):
            """Publish a block removed event."""
        
        def start_replay_service(self, port: int):
            """Start the replay service on specified port."""

Event Consumer (AIBrix)
~~~~~~~~~~~~~~~~~~~~~~~

.. code-block:: python

    class KVCacheEventConsumer:
        """Consumer for KV cache events in AIBrix."""
        
        def __init__(self, endpoints: List[str]):
            """Initialize the event consumer.
            
            Args:
                endpoints: List of ZMQ endpoints to connect to
            """
        
        def subscribe(self, callback: Callable):
            """Subscribe to events with a callback function."""
        
        def request_replay(self, endpoint: str, 
                         start_seq: int) -> List[Event]:
            """Request replay of events from a specific sequence."""

REST API Reference
------------------

Gateway Endpoints
~~~~~~~~~~~~~~~~~

Get Cache Status
^^^^^^^^^^^^^^^^

.. code-block:: text

    GET /api/v1/cache/status

**Response:**

.. code-block:: json

    {
        "enabled": true,
        "healthy": true,
        "stats": {
            "total_sequences": 150,
            "total_blocks": 4500,
            "total_tokens": 72000,
            "hit_rate": 0.85,
            "eviction_count": 120,
            "last_updated": 1704067200
        }
    }

Get Cached Sequences
^^^^^^^^^^^^^^^^^^^^

.. code-block:: text

    GET /api/v1/cache/sequences

**Response:**

.. code-block:: json

    {
        "sequences": [
            {
                "id": 12345,
                "token_count": 512,
                "block_count": 32,
                "last_accessed": 1704067200
            }
        ]
    }

Get Sequence Details
^^^^^^^^^^^^^^^^^^^^

.. code-block:: text

    GET /api/v1/cache/sequences/{sequence_id}

**Response:**

.. code-block:: json

    {
        "sequence_id": 12345,
        "tokens": [101, 102, 103, 104, 105],
        "blocks": [0, 1, 2, 3, 4],
        "metadata": {
            "created_at": 1704067100,
            "last_accessed": 1704067200,
            "access_count": 5
        }
    }

Clear Cache
^^^^^^^^^^^

.. code-block:: text

    POST /api/v1/cache/clear

**Response:**

.. code-block:: json

    {
        "success": true,
        "cleared_sequences": 150,
        "cleared_blocks": 4500
    }

WebSocket API
~~~~~~~~~~~~~

Event Stream
^^^^^^^^^^^^

.. code-block:: text

    WS /api/v1/cache/events

**Message Format:**

.. code-block:: json

    {
        "type": "block_stored",
        "timestamp": 1704067200,
        "data": {
            "sequence_id": 12345,
            "block_numbers": [0, 1, 2],
            "block_token_ids": [[101, 102], [103, 104], [105, 106]]
        }
    }

**Event Types:**

- ``block_stored``: New blocks cached
- ``block_removed``: Blocks evicted
- ``cache_cleared``: Cache was cleared
- ``stats_update``: Periodic stats update

Error Codes
-----------

.. list-table::
   :header-rows: 1
   :widths: 20 40 40

   * - Code
     - Name
     - Description
   * - 1001
     - ``ZMQ_CONNECTION_ERROR``
     - Failed to connect to ZMQ endpoint
   * - 1002
     - ``ZMQ_TIMEOUT``
     - Operation timed out
   * - 1003
     - ``INVALID_EVENT_FORMAT``
     - Event data is malformed
   * - 1004
     - ``REPLAY_FAILED``
     - Failed to replay events
   * - 1005
     - ``TOKENIZER_ERROR``
     - Remote tokenizer error
   * - 1006
     - ``CACHE_FULL``
     - Cache capacity exceeded
   * - 1007
     - ``SEQUENCE_NOT_FOUND``
     - Requested sequence not in cache

Best Practices
--------------

1. **Error Handling**: Always handle connection failures gracefully
2. **Timeouts**: Configure appropriate timeouts for your deployment
3. **Monitoring**: Track event processing metrics
4. **Buffer Sizes**: Tune buffer sizes based on workload
5. **Replay**: Use replay for recovery after disconnections

See Also
--------

- :doc:`/features/kv-event-sync`
- :doc:`/deployment/kv-event-sync-setup`
- :doc:`/testing/kv-event-sync-e2e`