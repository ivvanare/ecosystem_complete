<?php

namespace Tests\Unit;

use App\Events\OperationPerformed;
use App\Listeners\SendNotification;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Log;
use Tests\TestCase;

class SendNotificationTest extends TestCase
{
    /** @test */
    public function listener_stores_data_in_redis_cache(): void
    {
        Cache::flush();
        
        $event = new OperationPerformed(1500.50, 'Main Store');
        $listener = new SendNotification();
        
        $listener->handle($event);
        
        $cachedData = Cache::get("last_operation:{$event->storeName}");
        
        $this->assertNotNull($cachedData);
        $this->assertEquals(1500.50, $cachedData['amount']);
        $this->assertEquals('Main Store', $cachedData['store']);
        $this->assertArrayHasKey('processed_at', $cachedData);
    }

    /** @test */
    public function listener_increments_operations_count(): void
    {
        Cache::flush();
        
        $event = new OperationPerformed(100, 'Test Store');
        $listener = new SendNotification();
        
        $listener->handle($event);
        
        $count = Cache::get("operations_count:{$event->storeName}", 0);
        $this->assertEquals(1, $count);
    }

    /** @test */
    public function listener_implements_should_queue(): void
    {
        $listener = new SendNotification();
        $this->assertInstanceOf(\Illuminate\Contracts\Queue\ShouldQueue::class, $listener);
    }

    /** @test */
    public function listener_uses_rabbitmq_connection(): void
    {
        $listener = new SendNotification();
        $this->assertEquals('rabbitmq', $listener->connection);
    }
}
