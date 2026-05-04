<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{ $title ?? 'Dashboard - MelZone' }}</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            margin: 0;
            padding: 20px;
            background: #f5f5f5;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            background: white;
            padding: 30px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .header {
            border-bottom: 2px solid #e0e0e0;
            padding-bottom: 20px;
            margin-bottom: 30px;
        }
        h1 {
            color: #333;
            margin: 0 0 10px 0;
        }
        .subtitle {
            color: #666;
            margin: 0;
        }
        .trigger-section {
            background: #e8f5e9;
            padding: 20px;
            border-radius: 5px;
            margin-bottom: 30px;
            text-align: center;
        }
        .btn-primary {
            background: #2196F3;
            color: white;
            padding: 12px 30px;
            border: none;
            border-radius: 5px;
            font-size: 16px;
            cursor: pointer;
            transition: background 0.3s;
        }
        .btn-primary:hover {
            background: #1976D2;
        }
        .btn-primary:disabled {
            background: #ccc;
            cursor: not-allowed;
        }
        .tutorial-section {
            margin-bottom: 40px;
        }
        .tutorial-section h2 {
            color: #333;
            margin-bottom: 20px;
        }
        .stage {
            background: #f9f9f9;
            border-left: 4px solid #2196F3;
            padding: 20px;
            margin-bottom: 20px;
            border-radius: 4px;
        }
        .stage h3 {
            margin-top: 0;
            color: #1976D2;
        }
        .stage p {
            color: #555;
            line-height: 1.6;
        }
        .tech-details {
            background: #fff;
            border: 1px solid #ddd;
            padding: 15px;
            margin-top: 15px;
            border-radius: 4px;
        }
        .tech-details strong {
            color: #333;
        }
        .flow-diagram {
            background: #fff3e0;
            padding: 30px;
            border-radius: 5px;
            text-align: center;
            margin-bottom: 30px;
        }
        .flow-diagram h2 {
            color: #333;
            margin-bottom: 20px;
        }
        .flow-arrow {
            display: inline-block;
            padding: 15px 25px;
            background: #2196F3;
            color: white;
            border-radius: 5px;
            font-weight: bold;
            margin: 5px;
        }
        .arrow {
            display: inline-block;
            font-size: 24px;
            color: #666;
            margin: 0 10px;
        }
        .operations-section, .health-section {
            background: #f9f9f9;
            padding: 20px;
            border-radius: 5px;
            margin-bottom: 20px;
        }
        .operations-section h2, .health-section h2 {
            margin-top: 0;
            color: #333;
        }
        .operation-item {
            background: white;
            padding: 15px;
            margin-bottom: 10px;
            border-radius: 4px;
            border: 1px solid #ddd;
        }
        .status-online {
            color: #4caf50;
            font-weight: bold;
        }
        .status-offline {
            color: #f44336;
            font-weight: bold;
        }
        .fallback-message {
            background: #fff3cd;
            padding: 15px;
            border-radius: 4px;
            border: 1px solid #ffc107;
            color: #856404;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Dashboard - MelZone</h1>
            <p class="subtitle">Demostración interactiva del flujo RabbitMQ + Redis</p>
        </div>

        <div class="trigger-section">
            <h2>Enviar Notificación</h2>
            <p>Hacé click en el botón para disparar un evento de prueba</p>
            <form id="notification-form" method="POST" action="/test-operacion">
                <input type="hidden" name="_token" value="{{ csrf_token() }}">
                <button type="submit" class="btn-primary" id="send-notification-btn">
                    Enviar Notificación
                </button>
            </form>
        </div>

        <div class="flow-diagram">
            <h2>Diagrama de Flujo</h2>
            <div>
                <span class="flow-arrow">HTTP</span>
                <span class="arrow">→</span>
                <span class="flow-arrow">Event</span>
                <span class="arrow">→</span>
                <span class="flow-arrow">Queue</span>
                <span class="arrow">→</span>
                <span class="flow-arrow">Listener</span>
                <span class="arrow">→</span>
                <span class="flow-arrow">Cache</span>
            </div>
        </div>

        <div class="tutorial-section">
            <h2>Tutorial del Flujo - Paso a Paso</h2>

            <div class="stage">
                <h3>Paso 1: HTTP Request</h3>
                <p>El usuario hace click en "Enviar Notificación". Esto genera un request HTTP POST a la ruta configurada.</p>
                <div class="tech-details">
                    <strong>Ruta:</strong> /test-operacion<br>
                    <strong>Método:</strong> POST<br>
                    <strong>Parámetros:</strong> amount (monto aleatorio 100-9999), store (Tienda Central o Sucursal Norte)<br>
                    <strong>Controlador:</strong> OperationController@dispatchEvent
                </div>
            </div>

            <div class="stage">
                <h3>Paso 2: Event Dispatch</h3>
                <p>El controlador crea y dispara el evento OperationPerformed con los datos del request.</p>
                <div class="tech-details">
                    <strong>Evento:</strong> OperationPerformed<br>
                    <strong>Propiedades:</strong> amount, storeName, dispatchedAt<br>
                    <strong>Broadcast:</strong> broadcastOn() → canal dashboard-stats<br>
                    <strong>Queue:</strong> operacion.realizada
                </div>
            </div>

            <div class="stage">
                <h3>Paso 3: RabbitMQ Queue</h3>
                <p>El evento se serializa y se envía a la cola de RabbitMQ para procesamiento asíncrono.</p>
                <div class="tech-details">
                    <strong>Nombre de la cola:</strong> operacion.realizada<br>
                    <strong>Exchange:</strong> amq.direct<br>
                    <strong>Routing Key:</strong> operacion.realizada<br>
                    <strong>Payload:</strong> JSON con amount, store, dispatched_at, timestamp
                </div>
            </div>

            <div class="stage">
                <h3>Paso 4: Listener Processing</h3>
                <p>El listener SendNotification escucha la cola y procesa el evento de forma asíncrona.</p>
                <div class="tech-details">
                    <strong>Listener:</strong> SendNotification<br>
                    <strong>Método:</strong> handle(OperationPerformed $event)<br>
                    <strong>Acción:</strong> Maneja el evento, loggea en Laravel log<br>
                    <strong>Queue:</strong> Procesa desde operacion.realizada
                </div>
            </div>

            <div class="stage">
                <h3>Paso 5: Redis Cache</h3>
                <p>Una vez procesado el evento, los datos se guardan en Redis para acceso rápido.</p>
                <div class="tech-details">
                    <strong>Cache Key:</strong> last_operation:{storeName}<br>
                    <strong>TTL:</strong> 1 hora (3600 segundos)<br>
                    <strong>Estructura:</strong> amount, store, processed_at<br>
                    <strong>Driver:</strong> Redis (phpredis)
                </div>
            </div>
        </div>

        <div class="operations-section">
            <h2>Últimas 5 Operaciones</h2>
            @if(isset($lastOperations) && count($lastOperations) > 0)
                @foreach($lastOperations as $operation)
                    <div class="operation-item">
                        <strong>Tienda:</strong> {{ $operation['store'] ?? 'N/A' }}<br>
                        <strong>Monto:</strong> ${{ number_format($operation['amount'] ?? 0, 2) }}<br>
                        <strong>Procesado:</strong> {{ $operation['processed_at'] ?? 'N/A' }}
                    </div>
                @endforeach
            @else
                <div class="fallback-message">
                    Cache: No disponible o sin operaciones previas
                </div>
            @endif
        </div>

        <div class="health-section">
            <h2>Estado del Sistema</h2>
            <p><strong>Queue (RabbitMQ):</strong>
                @if(isset($rabbitmqStatus))
                    <span class="{{ $rabbitmqStatus === 'online' ? 'status-online' : 'status-offline' }}">
                        {{ ucfirst($rabbitmqStatus) }}
                    </span>
                @else
                    <span class="status-offline">Offline</span>
                @endif
                (operacion.realizada)
            </p>
        </div>
    </div>

    <script>
        document.getElementById('notification-form').addEventListener('submit', function(e) {
            e.preventDefault();

            const btn = document.getElementById('send-notification-btn');
            btn.disabled = true;
            btn.textContent = 'Enviando...';

            const formData = new FormData(this);

            fetch('/test-operacion', {
                method: 'POST',
                body: formData,
                headers: {
                    'X-Requested-With': 'XMLHttpRequest'
                }
            })
            .then(response => response.json())
            .then(data => {
                console.log('Notificación enviada:', data);
                setTimeout(() => {
                    window.location.reload();
                }, 1000);
            })
            .catch(error => {
                console.error('Error:', error);
                btn.disabled = false;
                btn.textContent = 'Enviar Notificación';
                alert('Error al enviar la notificación. Verificá que el servidor esté funcionando.');
            });
        });
    </script>
</body>
</html>
