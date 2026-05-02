<?php

namespace App\Listeners;

use App\Events\OperacionRealizada;
use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Log;

/**
 * EnviarNotificacion - Queue Listener (RabbitMQ → Redis Cache)
 * 
 * This listener is queued via RabbitMQ and processes the OperacionRealizada event.
 * It stores the operation data in Redis using Laravel's Cache facade.
 * 
 * Flow: Event dispatched → RabbitMQ queue → This listener picks up → Redis Cache updated
 */
class EnviarNotificacion implements ShouldQueue
{
    /**
     * The queue connection to use (RabbitMQ).
     *
     * @var string
     */
    public $connection = "rabbitmq";

    /**
     * Handle the event - process via queue and cache in Redis.
     *
     * @param OperacionRealizada $event
     * @return void
     */
    public function handle(OperacionRealizada $event): void
    {
        // Store operation in Redis cache using Laravel's Cache facade (phpredis backend)
        $cacheKey = "last_operation:{$event->storeName}";
        
        Cache::put($cacheKey, [
            'amount' => $event->amount,
            'store' => $event->storeName,
            'processed_at' => now()->toISOString(),
        ], now()->addHour());

        // Optional: increment a counter for total operations per store
        Cache::increment("operations_count:{$event->storeName}");

        Log::info("Operation processed and cached", [
            'store' => $event->storeName,
            'amount' => $event->amount,
            'cache_key' => $cacheKey,
        ]);
    }
}
