package app.mediacore.phone

import android.app.Application
import app.mediacore.core.Api
import coil3.ImageLoader
import coil3.PlatformContext
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import coil3.request.crossfade

/** Images share the API's connection to mint; full colour so dark backdrops don't band. */
class App : Application(), SingletonImageLoader.Factory {
    override fun newImageLoader(context: PlatformContext): ImageLoader =
        ImageLoader.Builder(context)
            .components { add(OkHttpNetworkFetcherFactory(callFactory = { Api.http })) }
            .crossfade(150)
            .build()
}
