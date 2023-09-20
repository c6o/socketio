const {Server} = require("socket.io");
const customParser = require('socket.io-msgpack-parser');

const io = new Server(2233, {
    // parser: customParser // enable binary msg
    pingInterval: 5000,
    pingTimeout: 3000,
});

io.on("connection", (socket) => {
    console.log('[server] client connected')

    // setTimeout(() => {
    //     socket.disconnect();
    // }, 5000);

    // ...
    socket.on('message', function (arg1, arg2, arg3) {
        console.log('[server] received message:', arg1, arg2, arg3)
    });


    socket.on("disconnect", (reason) => {
        console.log(`[server] disconnect because ${reason}`); // disconnect because transport close
    });

    // listen ack event
    socket.on('/ackFromClient', function (arg1, arg2, func) {
        console.log('[server] received ack:', arg1, arg2)
        func(1, {text: 'resp'}, "server")
    });

    socket.emit('message', {id: 2, channel: "{\"chinese\":\"中文才是最屌的\"}"})

    // emit ack event
    socket.emit('/ackFromServer', "vue", 3, function (arg1, arg2) {
        console.log('[server] ack cb:', arg1, arg2)
    });
});

console.log("[server] starting server...")

