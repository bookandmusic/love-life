package com.bookandmusic.lovelife.appnative.data

import android.content.Context
import androidx.core.content.edit
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * 凭据与令牌的安全存储（Android Keystore 加密）。
 * 降级语义：安全存储不可用时令牌只留内存（每次启动重登），凭据不保存、不落明文。
 */
object SecureStore {
    private const val FILE = "ll_secure"
    private const val KEY_TOKEN = "ll.token"
    private const val KEY_REFRESH = "ll.refresh"
    private const val KEY_USER = "ll.username"
    private const val KEY_PASS = "ll.password"

    private var prefs: SharedPreferences? = null
    private val memory = mutableMapOf<String, String>()

    fun init(context: Context) {
        if (prefs != null) return
        prefs = try {
            val master = MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build()
            EncryptedSharedPreferences.create(
                context, FILE, master,
                EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
            )
        } catch (_: Exception) {
            null // 降级：仅内存
        }
    }

    private fun get(key: String): String? = prefs?.getString(key, null) ?: memory[key]
    private fun put(key: String, value: String) {
        val p = prefs
        if (p != null) p.edit { putString(key, value) } else memory[key] = value
    }
    private fun remove(key: String) {
        prefs?.edit { remove(key) }
        memory.remove(key)
    }

    fun token(): String? = get(KEY_TOKEN)
    fun refreshToken(): String? = get(KEY_REFRESH)
    fun setTokens(token: String, refresh: String) {
        put(KEY_TOKEN, token)
        put(KEY_REFRESH, refresh)
    }
    fun clearTokens() {
        remove(KEY_TOKEN)
        remove(KEY_REFRESH)
    }

    fun saveCredentials(username: String, password: String): Boolean {
        put(KEY_USER, username)
        put(KEY_PASS, password)
        return prefs != null
    }
    fun loadCredentials(): Pair<String, String>? {
        val u = get(KEY_USER) ?: return null
        val pass = get(KEY_PASS) ?: return null
        return u to pass
    }
    fun clearCredentials() {
        remove(KEY_USER)
        remove(KEY_PASS)
    }
}
