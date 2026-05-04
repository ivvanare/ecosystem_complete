<?php

namespace Tests\Feature;

use App\Events\OperationPerformed;
use Illuminate\Support\Facades\Event;
use Tests\TestCase;

class OperationControllerTest extends TestCase
{
    /** @test */
    public function dashboard_route_returns_200(): void
    {
        $response = $this->get('/dashboard');

        $response->assertStatus(200);
        $response->assertSee('Dashboard');
    }

    /** @test */
    public function post_test_operacion_returns_json_with_event_details(): void
    {
        Event::fake();

        $response = $this->postJson('/test-operacion', [
            'amount' => 2500.75,
            'store' => 'Tienda Central',
        ]);

        $response->assertStatus(200);
        $response->assertJsonStructure([
            'status',
            'queue',
            'event_data' => [
                'amount',
                'store',
                'dispatched_at',
            ],
            'cache_key',
        ]);

        $response->assertJson([
            'status' => 'dispatched',
            'queue' => 'operacion.realizada',
        ]);

        $responseData = $response->json();
        $this->assertEquals(2500.75, $responseData['event_data']['amount']);
        $this->assertEquals('Tienda Central', $responseData['event_data']['store']);
        $this->assertNotNull($responseData['event_data']['dispatched_at']);
        $this->assertEquals('last_operation:Tienda Central', $responseData['cache_key']);
    }

    /** @test */
    public function post_test_operacion_dispatches_operation_performed_event(): void
    {
        Event::fake();

        $this->postJson('/test-operacion', [
            'amount' => 1500,
            'store' => 'Sucursal Norte',
        ]);

        Event::assertDispatched(OperationPerformed::class, function ($event) {
            return $event->amount == 1500 && $event->storeName === 'Sucursal Norte';
        });
    }

    /** @test */
    public function post_test_operacion_with_random_data_generates_different_responses(): void
    {
        Event::fake();

        $response1 = $this->postJson('/test-operacion', [
            'amount' => 1000,
            'store' => 'Tienda Central',
        ]);

        $response2 = $this->postJson('/test-operacion', [
            'amount' => 5000,
            'store' => 'Sucursal Norte',
        ]);

        $data1 = $response1->json();
        $data2 = $response2->json();

        $this->assertEquals(1000, $data1['event_data']['amount']);
        $this->assertEquals(5000, $data2['event_data']['amount']);
        $this->assertEquals('Tienda Central', $data1['event_data']['store']);
        $this->assertEquals('Sucursal Norte', $data2['event_data']['store']);
    }

    /** @test */
    public function get_test_operacion_still_works_for_backward_compatibility(): void
    {
        Event::fake();

        $response = $this->get('/test-operacion');

        $response->assertStatus(200);
        $response->assertSee('Evento disparado a RabbitMQ');
    }
}
