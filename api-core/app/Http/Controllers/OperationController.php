<?php

namespace App\Http\Controllers;

use App\Events\OperationPerformed;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Cache;

class OperationController extends Controller
{
    /**
     * Dispatch OperationPerformed event via POST request
     *
     * @param Request $request
     * @return \Illuminate\Http\JsonResponse
     */
    public function dispatchEvent(Request $request)
    {
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
     *
     * @return string
     */
    private function getRandomStore(): string
    {
        $stores = ['Tienda Central', 'Sucursal Norte', 'Sucursal Sur'];
        return $stores[array_rand($stores)];
    }
}
