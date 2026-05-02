<?php

use Illuminate\Support\Facades\Route;
use Illuminate\Support\Facades\Cache;
use App\Events\OperacionRealizada;

/*
|--------------------------------------------------------------------------
| Web Routes - Queue (RabbitMQ) + Cache (Redis) Example
|--------------------------------------------------------------------------
|
| This example demonstrates the full flow:
| HTTP Request → Event Dispatch → RabbitMQ Queue → Listener → Redis Cache
|
*/

// Welcome page
Route::get("/", function () {
    return view("welcome");
});

// Dispatch event to RabbitMQ queue
// The EnviarNotificacion listener will process it and store data in Redis
Route::get("/test-operacion", function () {
    // Disparamos evento que será procesado por RabbitMQ
    // El listener guardará los datos en Redis Cache
    event(new OperacionRealizada(1500.5, "Tienda Central"));
    
    return "✅ Evento disparado a RabbitMQ. El listener procesará y guardará en Redis.";
});

// Check cached data in Redis
// Usage: /check-cache/Tienda Central
Route::get("/check-cache/{store?}", function (?string $store = null) {
    if ($store) {
        $data = Cache::get("last_operation:{$store}");
        $count = Cache::get("operations_count:{$store}", 0);
        
        return response()->json([
            'store' => $store,
            'last_operation' => $data,
            'total_operations' => $count,
        ]);
    }
    
    // Show all cached operations
    return response()->json([
        'message' => 'Use /check-cache/{storeName} to see cached data',
        'example' => '/check-cache/Tienda Central',
    ]);
});
