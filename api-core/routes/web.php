<?php

use App\Http\Controllers\DashboardController;
use App\Http\Controllers\OperationController;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Route;

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
Route::get('/', function () {
    return view('welcome');
});

// Dispatch event to RabbitMQ queue - POST (new) and GET (backward compatibility)
Route::match(['get', 'post'], '/test-operacion', [OperationController::class, 'dispatchEvent'])
    ->name('operation.dispatch');

// Dashboard route (Phase 2 - base structure)
Route::get('/dashboard', [DashboardController::class, 'index'])
    ->name('dashboard.index');

// Check cached data in Redis
// Usage: /check-cache/Tienda Central
Route::get('/check-cache/{store?}', function (?string $store = null) {
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
