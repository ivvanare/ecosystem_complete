<?php

namespace Tests\Unit;

use App\Events\OperationPerformed;
use Illuminate\Broadcasting\Channel;
use Tests\TestCase;

class OperationPerformedTest extends TestCase
{
    /** @test */
    public function event_has_correct_properties(): void
    {
        $amount = 1500.50;
        $storeName = 'Main Store';
        $event = new OperationPerformed($amount, $storeName);

        $this->assertEquals($amount, $event->amount);
        $this->assertEquals($storeName, $event->storeName);
        $this->assertNotNull($event->dispatchedAt);
    }

    /** @test */
    public function event_broadcasts_on_correct_channel(): void
    {
        $event = new OperationPerformed(100, 'Store');
        $channels = $event->broadcastOn();

        $this->assertCount(1, $channels);
        $this->assertInstanceOf(Channel::class, $channels[0]);
        $this->assertEquals('dashboard-stats', $channels[0]->name);
    }

    /** @test */
    public function event_broadcasts_with_correct_data(): void
    {
        $event = new OperationPerformed(2000, 'Central Store');
        $data = $event->broadcastWith();

        $this->assertArrayHasKey('amount', $data);
        $this->assertArrayHasKey('store', $data);
        $this->assertArrayHasKey('dispatched_at', $data);
        $this->assertArrayHasKey('timestamp', $data);
        $this->assertEquals(2000, $data['amount']);
        $this->assertEquals('Central Store', $data['store']);
    }

    /** @test */
    public function event_implements_should_broadcast(): void
    {
        $event = new OperationPerformed(100, 'Store');
        $this->assertInstanceOf(\Illuminate\Contracts\Broadcasting\ShouldBroadcast::class, $event);
    }
}
