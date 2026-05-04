<?php

namespace App\Http\Controllers;

use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Log;

class DashboardController extends Controller
{
    /**
     * Display the dashboard.
     *
     * @return \Illuminate\View\View
     */
    public function index()
    {
        $rabbitmqStatus = $this->checkRabbitMqStatus();
        $lastOperations = $this->getLastOperations(5);
        $tutorialStages = $this->getTutorialStages();

        return view('dashboard', [
            'title' => 'Dashboard - MelZone',
            'rabbitmqStatus' => $rabbitmqStatus,
            'lastOperations' => $lastOperations,
            'tutorialStages' => $tutorialStages,
        ]);
    }

    /**
     * Check RabbitMQ connection status.
     *
     * @return string 'online' or 'offline'
     */
    private function checkRabbitMqStatus(): string
    {
        try {
            if (!class_exists(\PhpAmqpLib\Connection\AMQPStreamConnection::class)) {
                Log::warning('PhpAmqpLib not available for RabbitMQ status check');
                return 'offline';
            }

            $connection = new \PhpAmqpLib\Connection\AMQPStreamConnection(
                config('queue.connections.rabbitmq.host', 'localhost'),
                config('queue.connections.rabbitmq.port', 5672),
                config('queue.connections.rabbitmq.user', 'guest'),
                config('queue.connections.rabbitmq.password', 'guest'),
                config('queue.connections.rabbitmq.vhost', '/')
            );

            $connection->close();
            return 'online';
        } catch (\Exception $e) {
            Log::warning('RabbitMQ status check failed: ' . $e->getMessage());
            return 'offline';
        }
    }

    /**
     * Get the last N operations from Redis cache.
     *
     * @param int $limit
     * @return array
     */
    private function getLastOperations(int $limit): array
    {
        try {
            $operations = [];
            $stores = ['Tienda Central', 'Sucursal Norte'];

            foreach ($stores as $store) {
                $data = Cache::get("last_operation:{$store}");
                if ($data && is_array($data)) {
                    $operations[] = $data;
                }
            }

            // Sort by processed_at descending
            usort($operations, function ($a, $b) {
                $timeA = strtotime($a['processed_at'] ?? '1970-01-01');
                $timeB = strtotime($b['processed_at'] ?? '1970-01-01');
                return $timeB - $timeA;
            });

            return array_slice($operations, 0, $limit);
        } catch (\Exception $e) {
            Log::warning('Failed to retrieve operations from cache: ' . $e->getMessage());
            return [];
        }
    }

    /**
     * Get tutorial stages for the dashboard.
     *
     * @return array
     */
    private function getTutorialStages(): array
    {
        return [
            [
                'stage' => 1,
                'title' => 'HTTP Request',
                'description' => 'El usuario hace click en "Enviar Notificación". Esto genera un request HTTP POST a la ruta configurada.',
                'tech_details' => [
                    'route' => '/test-operacion',
                    'method' => 'POST',
                    'params' => 'amount (monto aleatorio 100-9999), store (Tienda Central o Sucursal Norte)',
                    'controller' => 'OperationController@dispatchEvent',
                ],
            ],
            [
                'stage' => 2,
                'title' => 'Event Dispatch',
                'description' => 'El controlador crea y dispara el evento OperationPerformed con los datos del request.',
                'tech_details' => [
                    'event' => 'OperationPerformed',
                    'properties' => 'amount, storeName, dispatchedAt',
                    'broadcast' => "broadcastOn() → canal dashboard-stats",
                    'queue' => 'operacion.realizada',
                ],
            ],
            [
                'stage' => 3,
                'title' => 'RabbitMQ Queue',
                'description' => 'El evento se serializa y se envía a la cola de RabbitMQ para procesamiento asíncrono.',
                'tech_details' => [
                    'queue_name' => 'operacion.realizada',
                    'exchange' => 'amq.direct',
                    'routing_key' => 'operacion.realizada',
                    'payload' => 'JSON con amount, store, dispatched_at, timestamp',
                ],
            ],
            [
                'stage' => 4,
                'title' => 'Listener Processing',
                'description' => 'El listener SendNotification escucha la cola y procesa el evento de forma asíncrona.',
                'tech_details' => [
                    'listener' => 'SendNotification',
                    'method' => 'handle(OperationPerformed $event)',
                    'action' => 'Maneja el evento, loggea en Laravel log',
                    'queue' => 'Procesa desde operacion.realizada',
                ],
            ],
            [
                'stage' => 5,
                'title' => 'Redis Cache',
                'description' => 'Una vez procesado el evento, los datos se guardan en Redis para acceso rápido.',
                'tech_details' => [
                    'cache_key' => 'last_operation:{storeName}',
                    'ttl' => '1 hora (3600 segundos)',
                    'structure' => 'amount, store, processed_at',
                    'driver' => 'Redis (phpredis)',
                ],
            ],
        ];
    }
}
