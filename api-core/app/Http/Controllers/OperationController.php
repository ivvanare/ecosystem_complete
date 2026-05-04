<?php

namespace App\Http\Controllers;

use App\Events\OperationPerformed;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Http\Response;

class OperationController extends Controller
{
    /**
     * Dispatch OperationPerformed event
     * GET: Backward compatible (returns text, fixed values)
     * POST: Returns JSON with event details
     *
     * @return Response|JsonResponse
     */
    public function dispatchEvent(Request $request)
    {
        // Backward compatibility: GET returns text response
        if ($request->isMethod('get')) {
            event(new OperationPerformed(1500.5, 'Tienda Central'));

            return response('✅ Evento disparado a RabbitMQ. El listener procesará y guardará en Redis.');
        }

        // POST: Return JSON with event details
        $amount = $request->input('amount', rand(100, 9999));
        $storeName = $request->input('store', $this->getRandomStore());

        $cacheKey = "last_operation:{$storeName}";

        event(new OperationPerformed($amount, $storeName));

        return response()->json([
            'status' => 'dispatched',
            'queue' => 'operacion.realizada',
            'event_data' => [
                'amount' => $amount,
                'store' => $storeName,
                'dispatched_at' => now()->toDateTimeString(),
            ],
            'cache_key' => $cacheKey,
        ]);
    }

    /**
     * Get a random store name
     */
    private function getRandomStore(): string
    {
        $stores = ['Tienda Central', 'Sucursal Norte', 'Sucursal Sur'];

        return $stores[array_rand($stores)];
    }
}
