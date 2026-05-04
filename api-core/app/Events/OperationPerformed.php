<?php

namespace App\Events;

use Illuminate\Broadcasting\Channel;
use Illuminate\Broadcasting\InteractsWithSockets;
use Illuminate\Contracts\Broadcasting\ShouldBroadcast;
use Illuminate\Foundation\Events\Dispatchable;
use Illuminate\Queue\SerializesModels;

/**
 * OperationPerformed - Event for Queue (RabbitMQ) communication
 *
 * This event is dispatched when an operation is performed.
 * It is processed asynchronously via RabbitMQ queue by the SendNotification listener.
 * The listener then stores the operation data in Redis cache.
 *
 * Flow: HTTP Request → Event Dispatch → RabbitMQ Queue → Listener → Redis Cache
 */
class OperationPerformed implements ShouldBroadcast
{
    use Dispatchable, InteractsWithSockets, SerializesModels;

    /**
     * The operation amount.
     *
     * @var float
     */
    public $amount;

    /**
     * The store name where the operation occurred.
     *
     * @var string
     */
    public $storeName;

    /**
     * The timestamp when the event was dispatched.
     *
     * @var string
     */
    public $dispatchedAt;

    /**
     * Create a new event instance.
     *
     * @param  float  $amount
     * @param  string  $storeName
     */
    public function __construct($amount, $storeName)
    {
        $this->amount = $amount;
        $this->storeName = $storeName;
        $this->dispatchedAt = now()->toDateTimeString();
    }

    public function broadcastOn(): array
    {
        // Public channel for admin dashboard
        return [
            new Channel('dashboard-stats'),
        ];
    }

    // Data to be received by the Dashboard
    public function broadcastWith(): array
    {
        return [
            'amount' => $this->amount,
            'store' => $this->storeName,
            'dispatched_at' => $this->dispatchedAt,
            'timestamp' => now()->toDateTimeString(),
        ];
    }
}
