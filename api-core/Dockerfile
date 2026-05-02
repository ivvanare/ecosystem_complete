FROM php:8.2-fpm-alpine

# 1. Instalar dependencias del sistema y librerías de desarrollo
# Añadimos linux-headers para solucionar el error de 'sockets'
RUN apk add --no-cache \
    postgresql-dev \
    libpng-dev \
    libzip-dev \
    freetype-dev \
    libjpeg-turbo-dev \
    icu-dev \
    oniguruma-dev \
    zlib-dev \
    linux-headers \
    $PHPIZE_DEPS \
    zip \
    unzip \
    git

# 2. Configurar e instalar extensiones base de PHP
RUN docker-php-ext-configure gd --with-freetype --with-jpeg \
    && docker-php-ext-install \
    pdo_pgsql \
    pgsql \
    bcmath \
    gd \
    zip \
    sockets \
    intl \
    opcache

# 3. Instalar la extensión de Redis vía PECL (Alto rendimiento)
# Esto es lo que permite que PHP vuele al usar Redis
RUN pecl install redis \
    && docker-php-ext-enable redis

# 4. Instalar Composer
COPY --from=composer:latest /usr/bin/composer /usr/bin/composer

WORKDIR /var/www

EXPOSE 9000
