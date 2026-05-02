<?php

namespace App\Events;

use Illuminate\Foundation\Events\Dispatchable;
use Illuminate\Queue\SerializesModels;

/**
 * OperacionRealizada - Event for Queue (RabbitMQ) communication
 * 
 * This event is dispatched when an operation is performed.
 * It is processed asynchronously via RabbitMQ queue by the EnviarNotificacion listener.
 * The listener then stores the operation data in Redis cache.
 * 
 * Flow: HTTP Request → Event Dispatch → RabbitMQ Queue → Listener → Redis Cache
 */
class OperacionRealizada
{
    use Dispatchable, SerializesModels;

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
     * Create a new event instance.
     *
     * @param float $amount
     * @param string $storeName
     */
    public function __construct($amount, $storeName)
    {
        $this->amount = $amount;
        $this->storeName = $storeName;
    }
}
